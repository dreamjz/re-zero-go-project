#!/usr/bin/env sh
# #!/bin/sh
# 通过/usr/bin/env运行命令的好处是可以查找当前 environment 中程序的默认版本

# trap 收到 EXIT 信号时，执行命令
# rm server 移除临时文件
# kill 0 关闭当前进程组的所有进程
trap "rm server; kill 0" EXIT

go build -ldflags "-s -w" -o ./server
./server -port 8001  &
./server -port 8002 &
./server -port 8003 -api  &

sleep 1

echo ">>> start test"

curl "http://localhost:9999/api?key=Tom" &
curl "http://localhost:9999/api?key=Tom" &
curl "http://localhost:9999/api?key=Tom" &
curl "http://localhost:9999/api?key=Tom" &
curl "http://localhost:9999/api?key=Tom" &
curl "http://localhost:9999/api?key=Tom" &
curl "http://localhost:9999/api?key=Tom" &
curl "http://localhost:9999/api?key=Tom" &
curl "http://localhost:9999/api?key=Tom" &
curl "http://localhost:9999/api?key=Tom" &

wait
