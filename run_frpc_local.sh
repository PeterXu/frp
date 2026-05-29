pkill frpc
sleep 1
pkill -9 frpc
sleep 1
rm -f nohup.out
rm -f /var/log/frp/frpc.log
nohup ./bin/frpc -c ./frpc-local.toml &
