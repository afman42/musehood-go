#!/bin/bash
# localhost.run tunnel for Musehood Go (port 3001).
# Subdomain acak tiap reconnect: setiap URL baru terdeteksi, file
# trusted-hosts.env diperbarui dan musehood-go di-restart agar host
# baru diterima. URL aktif ditulis ke current-public-url.txt.
set -a; source /home/hatch/muse-secrets/proxy.env; set +a
export NO_PROXY=localhost,127.0.0.1 no_proxy=localhost,127.0.0.1
URL_FILE=/home/hatch/workspace/musehood-go/current-public-url.txt
ENV_FILE=/home/hatch/workspace/musehood-go/trusted-hosts.env
LOG=/home/hatch/workspace/musehood-go/lhr-tunnel.log
PC="/home/hatch/workspace/goals/stack-9router-hermes-bridge-muse/hidden_files/ssh-proxy.py %h %p"
while true; do
  /usr/bin/ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
    -o ProxyCommand="$PC" -o ServerAliveInterval=15 -o ServerAliveCountMax=3 \
    -R 80:localhost:3001 nokey@localhost.run < /dev/null 2>&1 | while IFS= read -r line; do
      echo "$(date -u +%FT%TZ) $line" >> "$LOG"
      URL=$(echo "$line" | grep -oE "https://[a-zA-Z0-9.-]+\.lhr\.life" | head -1)
      if [ -n "$URL" ]; then
        HOST=$(echo "$URL" | sed 's|https://||')
        echo "$URL" > "$URL_FILE"
        echo "TRUSTED_HOSTS=$HOST,crisp-star-bdba159c.tunnl.gg" > "$ENV_FILE"
        systemctl restart musehood-go.service
        echo "$(date -u +%FT%TZ) URL aktif: $URL (musehood-go di-restart)" >> "$LOG"
      fi
    done
  echo "$(date -u +%FT%TZ) tunnel ssh exited — restarting in 5s" >> "$LOG"
  sleep 5
done
