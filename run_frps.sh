pkill frps
sleep 1
pkill -9 frps
sleep 1
rm -f nohup.out
nohup ./bin/frps -c conf/frps-socks5.conf &
