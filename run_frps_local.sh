pkill frps
sleep 1
pkill -9 frps
sleep 1
rm -f nohup.out
rm -f /var/log/frp/frps.log
nohup ./bin/frps -c ./frps-local.toml &
