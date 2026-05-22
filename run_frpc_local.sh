pkill frpc
sleep 1
pkill -9 frpc
sleep 1
rm -f nohup.out
nohup ./bin/frpc -c ./frpc-local.toml &
