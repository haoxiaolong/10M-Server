The C10M Problem is about how on a modern server, you should be able to easily handle 10M concurrent connections with solid throughput and low jitter. Handling that level of traffic generally requires a more specialized approach than is offered by a stock Linux kernel.
Using a stock debian-8 image and a Go server you can handle 10M concurrent connections with low throughput and moderate jitter if the connections are mostly idle. The server design for this example is just about the simplest websocket server that is useful for anything. It is similar to a push notification server like the iOS Apple Push Notification Service, but without the ability to store messages if the client is offline.
The server accepts websocket connections ports 10000-11000 (to avoid exhaustion of ephemeral ports on the clients during testing) and in the url the client specifies a channel to connect to, such as:
ws://<server>:10000/<channel>
After the websocket connection has been setup, the server never reads any data from the connection, it only writes messages to the client. Publishing to the channel is handled by redis, using the PUBLISH/PSUBSCRIBE commands. This is unnecessary for a single server machine, but is nice when you have multiple servers and you need some sort of central place to handle message routing.
Whenever a message is published on a channel, the server will send a message to each connected client subscribed to that channel. To make sure clients are still connected, the server will also send a ping message every 5 minutes. The client can use a missing ping message to detect if it has been disconnected.
download
func handleConnection(ws *websocket.Conn, channel string) {
	sub := subscribe(channel)
	t := time.NewTicker(pingPeriod)

	var message []byte

	for {
		select {
		case <-t.C:
			message = nil
		case message = <-sub:
		}

		ws.SetWriteDeadline(time.Now().Add(30 * time.Second))
		err := ws.WriteMessage(websocket.TextMessage, message)
		if err != nil {
			break
		}
	}

	t.Stop()
	ws.Close()
	unsubscribe(channel, sub)
}
go get goroutines.com/10m-server
You can run the server like this:
apt-get update
apt-get install -y redis-server
echo "bind *" >> /etc/redis/redis.conf
systemctl restart redis-server
sysctl -w fs.file-max=11000000
sysctl -w fs.nr_open=11000000
ulimit -n 11000000
sysctl -w net.ipv4.tcp_mem="100000000 100000000 100000000"
sysctl -w net.core.somaxconn=10000
sysctl -w net.ipv4.tcp_max_syn_backlog=10000
10m-server
The client connects to a server specified on the command line and makes a number of connections also specified on the command line. It starts on port 10000 and increments the port for every 50k connections.
download
func createConnection(url string) {
	ws, _, err := dialer.Dial(url, nil)
	if err != nil {
		return
	}

	ws.SetReadLimit(maxMessageSize)

	for {
		ws.SetReadDeadline(time.Now().Add(idleTimeout))
		_, message, err := ws.ReadMessage()
		if err != nil {
			break
		}
		if len(message) > 0 {
			fmt.Println("received message", url, string(message))
		}
	}

	ws.Close()
}
go get goroutines.com/10m-client
You can run the client like this:
sysctl -w fs.file-max=11000000
sysctl -w fs.nr_open=11000000
ulimit -n 11000000
sysctl -w net.ipv4.ip_local_port_range="1025 65535"
sysctl -w net.ipv4.tcp_mem="100000000 100000000 100000000"
10m-client <ip address> <number of connections>
This server was run on an n1-highmem-32 instance on GCE. This is a 32-core machine with 208GB of memory. Sending a ping every 5 minutes at 10M connections was roughly the limit of what the server could handle. This ends up being only about 30k pings per second, which is not a terribly high number. It seems to be limited by the kernel or network settings, as using 8 4-core machines (the same number of cores) could handle at least 5x the pings per second that a single 32-core machine could. Since the connections are mostly idle, the channel message traffic is assumed to be insignificant compared to the pings.
At the full 10M connections, the server's CPUs are only at 10% load and memory is only half used with the default GOGC=100, so it's likely that the hardware could handle 20M connections and a much higher ping rate without any fancy optimizations to the server code. The garbage collector is surprisingly performant, even with 100GB of memory allocated to the process.
By using smaller instances, such as n1-highmem-4 instances, with 1.3M connections each and putting them behind Google's excellent layer-3 load balancer you can more easily scale to whatever the maximum number of connections allowed for the load balancer is, if it's limited at all.
It's likely a larger number of concurrent connections could be handled using a user space tcp stack such as mTCP or a direct interface to the network card like DPDK though it's unclear how hard those would be to integrate with Go since they may require pinning threads to specific cores, for instance.
