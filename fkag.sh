#!/bin/bash
#
# 域名级透明代理原型验证
#
# 将 config.yaml 中的域名通过 /etc/resolver/ 劫持到本地 DNS，
# 本地 DNS 返回 loopback alias IP，TCP 透传经由 HTTP 代理转发。
#
# 用法：
#   sudo ./scripts/proxy_domains.sh
#
# 启动后去使用目标应用验证，按 Enter 停止并清理。

set -e

CONFIG_FILE="config.yaml"
RESOLVER_DIR="/etc/resolver"
PROXY_HOST="127.0.0.1"
PROXY_PORT="7897"
DNS_PORT=10053
VIP_BASE=2  # 虚拟 IP 从 127.0.0.2 开始

if [ "$(id -u)" -ne 0 ]; then
    echo "需要 sudo 权限"
    exit 1
fi

if [ ! -f "${CONFIG_FILE}" ]; then
    echo "找不到 ${CONFIG_FILE}"
    exit 1
fi

# 解析 config.yaml 中的 domains 列表（无需 yq，纯 grep+sed）
DOMAINS=()
while IFS= read -r line; do
    line=$(echo "${line}" | xargs)
    [ -n "${line}" ] && DOMAINS+=("${line}")
done < <(grep -A9999 '^domains:' "${CONFIG_FILE}" | tail -n +2 | sed -n 's/^[[:space:]]*-[[:space:]]*//p')

if [ ${#DOMAINS[@]} -eq 0 ]; then
    echo "config.yaml 中 domains 列表为空"
    exit 1
fi

# 构建域名 -> 虚拟 IP 映射
VIPS=()
idx=${VIP_BASE}
for domain in "${DOMAINS[@]}"; do
    VIPS+=("127.0.0.${idx}")
    idx=$((idx + 1))
done

CREATED_FILES=()
ADDED_ALIASES=()
PIDS=()

cleanup() {
    echo ""
    echo "--- 清理 ---"

    # 停所有后台进程
    for pid in "${PIDS[@]}"; do
        kill "${pid}" 2>/dev/null || true
        wait "${pid}" 2>/dev/null || true
    done

    # 卸载 pf anchor
    pfctl -a com.reentry -F all 2>/dev/null || true
    echo "pf anchor 已卸载"

    # 删 resolver 文件
    for f in "${CREATED_FILES[@]}"; do
        rm -f "${f}" && echo "已删除 ${f}"
    done

    # 移除 loopback alias
    for vip in "${ADDED_ALIASES[@]}"; do
        ifconfig lo0 -alias "${vip}" 2>/dev/null || true
    done
    echo "loopback alias 已移除"

    # 刷 DNS 缓存
    dscacheutil -flushcache 2>/dev/null || true
    killall -HUP mDNSResponder 2>/dev/null || true
    echo "DNS 缓存已刷新，已退出"
}
trap cleanup EXIT

# --- 步骤 1：添加 loopback alias ---
echo "=== 添加 loopback alias ==="
for i in "${!DOMAINS[@]}"; do
    vip="${VIPS[$i]}"
    ifconfig lo0 alias "${vip}" 2>/dev/null || true
    ADDED_ALIASES+=("${vip}")
    echo "  lo0 alias ${vip} -> ${DOMAINS[$i]}"
done

# --- 步骤 2：启动本地 DNS 服务器 ---
echo ""
echo "=== 启动本地 DNS 服务器 (127.0.0.1:${DNS_PORT}) ==="

# 生成 DNS 映射
DNS_MAP="{"
for i in "${!DOMAINS[@]}"; do
    DNS_MAP+="'${DOMAINS[$i]}': '${VIPS[$i]}',"
done
DNS_MAP+="}"

python3 -c "
import socket, struct

MAPPING = ${DNS_MAP}

def build_response(data, mapping):
    txid = data[:2]
    pos = 12
    labels = []
    while data[pos] != 0:
        length = data[pos]
        pos += 1
        labels.append(data[pos:pos+length].decode())
        pos += length
    pos += 1
    qname = '.'.join(labels)
    qtype = struct.unpack('!H', data[pos:pos+2])[0]

    if qtype == 1 and qname in mapping:
        ip = mapping[qname]
        flags = b'\x81\x80'
        ancount = b'\x00\x01'
        answer = b'\xc0\x0c\x00\x01\x00\x01' + struct.pack('!I', 60) + b'\x00\x04' + socket.inet_aton(ip)
        response = txid + flags + b'\x00\x01' + ancount + b'\x00\x00\x00\x00'
        response += data[12:pos+4] + answer
        print(f'  DNS: {qname} -> {ip}', flush=True)
        return response

    flags = b'\x81\x83'
    response = txid + flags + b'\x00\x01\x00\x00\x00\x00\x00\x00'
    response += data[12:pos+4]
    print(f'  DNS: {qname} -> NXDOMAIN', flush=True)
    return response

sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
sock.bind(('127.0.0.1', ${DNS_PORT}))
print('DNS 服务器已启动', flush=True)

while True:
    data, addr = sock.recvfrom(512)
    try:
        resp = build_response(data, MAPPING)
        sock.sendto(resp, addr)
    except Exception as e:
        print(f'  DNS 错误: {e}', flush=True)
" &
PIDS+=($!)
sleep 0.5

# --- 步骤 3：启动 TCP 透传代理 ---
echo ""
echo "=== 启动 TCP 透传代理 ==="

for i in "${!DOMAINS[@]}"; do
    domain="${DOMAINS[$i]}"
    vip="${VIPS[$i]}"

    for port in 443 80; do
        listen_port=$((port + 10000))

        python3 -c "
import socket, threading

LISTEN_IP = '${vip}'
LISTEN_PORT = ${listen_port}
TARGET_HOST = '${domain}'
TARGET_PORT = ${port}
PROXY_HOST = '${PROXY_HOST}'
PROXY_PORT = ${PROXY_PORT}

def handle_client(client_sock):
    try:
        proxy_sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        proxy_sock.settimeout(10)
        proxy_sock.connect((PROXY_HOST, PROXY_PORT))

        connect_req = f'CONNECT {TARGET_HOST}:{TARGET_PORT} HTTP/1.1\r\nHost: {TARGET_HOST}:{TARGET_PORT}\r\n\r\n'
        proxy_sock.sendall(connect_req.encode())

        response = b''
        while b'\r\n\r\n' not in response:
            chunk = proxy_sock.recv(4096)
            if not chunk:
                break
            response += chunk

        if b'200' not in response.split(b'\r\n')[0]:
            print(f'  CONNECT 失败: {response.split(chr(13).encode())[0]}', flush=True)
            client_sock.close()
            proxy_sock.close()
            return

        proxy_sock.settimeout(None)
        client_sock.settimeout(None)

        def forward(src, dst):
            try:
                while True:
                    data = src.recv(65536)
                    if not data:
                        break
                    dst.sendall(data)
            except:
                pass
            finally:
                try: src.close()
                except: pass
                try: dst.close()
                except: pass

        t1 = threading.Thread(target=forward, args=(client_sock, proxy_sock), daemon=True)
        t2 = threading.Thread(target=forward, args=(proxy_sock, client_sock), daemon=True)
        t1.start()
        t2.start()
        t1.join()
        t2.join()
    except Exception as e:
        print(f'  转发错误 ({TARGET_HOST}:{TARGET_PORT}): {e}', flush=True)
        try: client_sock.close()
        except: pass

server = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
server.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
server.bind((LISTEN_IP, LISTEN_PORT))
server.listen(32)
print(f'  {LISTEN_IP}:{LISTEN_PORT} -> {TARGET_HOST}:{TARGET_PORT} (via proxy)', flush=True)

while True:
    client, addr = server.accept()
    threading.Thread(target=handle_client, args=(client,), daemon=True).start()
" &
        PIDS+=($!)
    done
done
sleep 0.5

# --- 步骤 4：加载 pf 规则 ---
echo ""
echo "=== 加载 pf 端口重定向规则 ==="

PF_RULES=""
for i in "${!DOMAINS[@]}"; do
    vip="${VIPS[$i]}"
    PF_RULES+="rdr on lo0 inet proto tcp from any to ${vip} port 443 -> ${vip} port 10443
"
    PF_RULES+="rdr on lo0 inet proto tcp from any to ${vip} port 80 -> ${vip} port 10080
"
done

echo "${PF_RULES}" | pfctl -a com.reentry -f - 2>/dev/null
pfctl -e 2>/dev/null || true
echo "  pf anchor com.reentry 已加载"

# --- 步骤 5：创建 resolver 文件 ---
echo ""
echo "=== 配置 DNS resolver ==="

mkdir -p "${RESOLVER_DIR}"
for i in "${!DOMAINS[@]}"; do
    domain="${DOMAINS[$i]}"
    vip="${VIPS[$i]}"
    # resolver 文件名不能含 *，通配符域名用 _wildcard_ 替代
    safe_name=$(echo "${domain}" | sed 's/\*/_wildcard_/g')
    file="${RESOLVER_DIR}/${safe_name}"
    printf "nameserver 127.0.0.1\nport %s\n" "${DNS_PORT}" > "${file}"
    CREATED_FILES+=("${file}")
    echo "  ${domain} -> ${vip}"
done

dscacheutil -flushcache 2>/dev/null || true
killall -HUP mDNSResponder 2>/dev/null || true

echo ""
echo "=== 代理已就绪 ==="
echo ""
echo "域名映射："
for i in "${!DOMAINS[@]}"; do
    echo "  ${DOMAINS[$i]} -> ${VIPS[$i]} -> proxy ${PROXY_HOST}:${PROXY_PORT}"
done
echo ""
echo "现在可以启动目标应用进行验证。"
echo "按 Enter 停止代理并清理..."
read -r
