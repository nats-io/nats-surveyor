# NATS Surveyor

A NATS Monitoring POC.  Eventually this will be moved into the `nats-io` organization.

This project uses the polling of NATS server `Statz` messages to generate data for
Prometheus, allowing a single exporter to connect to any NATS server and get an entire
picture of a NATS deployment.

## Usage

```text
Usage of ./nats-surveyor:
  -a string
    	Network host to listen on. (default "0.0.0.0")
  -addr string
    	Network host to listen on. (default "0.0.0.0")
  -c int
    	Expected number of servers (default 1)
  -creds string
    	Credentials File
  -http_pass string
    	Set the password for HTTP scrapes. NATS bcrypt supported.
  -http_user string
    	Enable basic auth and set user name for HTTP scrapes.
  -p int
    	Port to listen on. (default 7777)
  -port int
    	Port to listen on. (default 7777)
  -prefix string
    	Replace the default prefix for all the metrics.
  -s string
    	NATS Cluster url(s) (default "nats://localhost:4222")
  -timeout duration
    	Polling timeout (default 3s)
  -tlscacert string
    	Client certificate CA for verification (used with HTTPS).
  -tlscert string
    	Server certificate file (Enables HTTPS).
  -tlskey string
    	Private key for server certificate (used with HTTPS).
  -version
    	Show exporter version and exit.
```

At this time, NATS 2.0 System credentials are required for meaningful usage.

```sh
./nats-surveyor --creds ./test/SYS.creds
2019/10/14 21:35:40 Connected to NATS Deployment: 127.0.0.1:4222
2019/10/14 21:35:40 No certificate file specified; using http.
2019/10/14 21:35:40 Prometheus exporter listening at http://0.0.0.0:7777/metrics
```

## Metrics

Scrape output is the in form of nats_core_NNNN_metric, where NNN is `server`, `route`, or `gateway`.

To aid filtering, each metric has labels.  These include `nats_server_cluster`,
`nats_server_host`, `nats_server_id`.  Routes have additional flags, `nats_server_route_id`
and gatways have `nats_server_gateway_id` and `nats_server_gateway_name`.

The info metrics has a nats_server_version label with the current version.

Additionally, there is a `nats_up` metric that will normally return 1, but will return 0
and no additional NATS metrics when there is no connectivity to the NATS system.  This
allows users to differentiate between a problem with the exporter itself connectivity with
the NATS system.

### Scrape Output

```
nats_core_active_account_count{nats_server_cluster="region1",nats_server_host="localhost",nats_server_id="NDGERVW3RX7A6RAJQ34E7HPBFUD35322XRZJNTOMTFI7MHAXL2PS3OVW"} 2
nats_core_active_account_count{nats_server_cluster="region1",nats_server_host="localhost",nats_server_id="NDYW2PLO6QVP2VKKUMWGWJXBMPTZKB3UAYME26BTKOGLNN55NSEK3RQF"} 2
nats_core_active_account_count{nats_server_cluster="region2",nats_server_host="localhost",nats_server_id="NCBI75V5ASPJAEAR3VPS2YELXP7K6CUXXWAD5PB2SJ4BOIYQHU6JKV7A"} 2
# HELP nats_core_connection_count Current number of client connections gauge
# TYPE nats_core_connection_count gauge
nats_core_connection_count{nats_server_cluster="region1",nats_server_host="localhost",nats_server_id="NDGERVW3RX7A6RAJQ34E7HPBFUD35322XRZJNTOMTFI7MHAXL2PS3OVW"} 0
nats_core_connection_count{nats_server_cluster="region1",nats_server_host="localhost",nats_server_id="NDYW2PLO6QVP2VKKUMWGWJXBMPTZKB3UAYME26BTKOGLNN55NSEK3RQF"} 1
nats_core_connection_count{nats_server_cluster="region2",nats_server_host="localhost",nats_server_id="NCBI75V5ASPJAEAR3VPS2YELXP7K6CUXXWAD5PB2SJ4BOIYQHU6JKV7A"} 0
# HELP nats_core_core_count Machine cores gauge
# TYPE nats_core_core_count gauge
nats_core_core_count{nats_server_cluster="region1",nats_server_host="localhost",nats_server_id="NDGERVW3RX7A6RAJQ34E7HPBFUD35322XRZJNTOMTFI7MHAXL2PS3OVW"} 8
nats_core_core_count{nats_server_cluster="region1",nats_server_host="localhost",nats_server_id="NDYW2PLO6QVP2VKKUMWGWJXBMPTZKB3UAYME26BTKOGLNN55NSEK3RQF"} 8
nats_core_core_count{nats_server_cluster="region2",nats_server_host="localhost",nats_server_id="NCBI75V5ASPJAEAR3VPS2YELXP7K6CUXXWAD5PB2SJ4BOIYQHU6JKV7A"} 8
# HELP nats_core_cpu_percentage Server cpu utilization gauge
# TYPE nats_core_cpu_percentage gauge
nats_core_cpu_percentage{nats_server_cluster="region1",nats_server_host="localhost",nats_server_id="NDGERVW3RX7A6RAJQ34E7HPBFUD35322XRZJNTOMTFI7MHAXL2PS3OVW"} 0
nats_core_cpu_percentage{nats_server_cluster="region1",nats_server_host="localhost",nats_server_id="NDYW2PLO6QVP2VKKUMWGWJXBMPTZKB3UAYME26BTKOGLNN55NSEK3RQF"} 0
nats_core_cpu_percentage{nats_server_cluster="region2",nats_server_host="localhost",nats_server_id="NCBI75V5ASPJAEAR3VPS2YELXP7K6CUXXWAD5PB2SJ4BOIYQHU6JKV7A"} 0
# HELP nats_core_gateway_inbound_msg_count Number inbound messages through the gateway gauge
# TYPE nats_core_gateway_inbound_msg_count gauge
nats_core_gateway_inbound_msg_count{nats_server_cluster="region1",nats_server_gateway_id="7",nats_server_gateway_name="region2",nats_server_host="localhost",nats_server_id="NDGERVW3RX7A6RAJQ34E7HPBFUD35322XRZJNTOMTFI7MHAXL2PS3OVW"} 0
nats_core_gateway_inbound_msg_count{nats_server_cluster="region1",nats_server_gateway_id="9",nats_server_gateway_name="region2",nats_server_host="localhost",nats_server_id="NDYW2PLO6QVP2VKKUMWGWJXBMPTZKB3UAYME26BTKOGLNN55NSEK3RQF"} 1
nats_core_gateway_inbound_msg_count{nats_server_cluster="region2",nats_server_gateway_id="4",nats_server_gateway_name="region1",nats_server_host="localhost",nats_server_id="NCBI75V5ASPJAEAR3VPS2YELXP7K6CUXXWAD5PB2SJ4BOIYQHU6JKV7A"} 2
# HELP nats_core_gateway_recv_bytes Number of messages sent over the gateway gauge
# TYPE nats_core_gateway_recv_bytes gauge
nats_core_gateway_recv_bytes{nats_server_cluster="region1",nats_server_gateway_id="7",nats_server_gateway_name="region2",nats_server_host="localhost",nats_server_id="NDGERVW3RX7A6RAJQ34E7HPBFUD35322XRZJNTOMTFI7MHAXL2PS3OVW"} 0
nats_core_gateway_recv_bytes{nats_server_cluster="region1",nats_server_gateway_id="9",nats_server_gateway_name="region2",nats_server_host="localhost",nats_server_id="NDYW2PLO6QVP2VKKUMWGWJXBMPTZKB3UAYME26BTKOGLNN55NSEK3RQF"} 852
nats_core_gateway_recv_bytes{nats_server_cluster="region2",nats_server_gateway_id="4",nats_server_gateway_name="region1",nats_server_host="localhost",nats_server_id="NCBI75V5ASPJAEAR3VPS2YELXP7K6CUXXWAD5PB2SJ4BOIYQHU6JKV7A"} 4005
# HELP nats_core_gateway_recv_msg_count Number of messages sent over the gateway gauge
# TYPE nats_core_gateway_recv_msg_count gauge
nats_core_gateway_recv_msg_count{nats_server_cluster="region1",nats_server_gateway_id="7",nats_server_gateway_name="region2",nats_server_host="localhost",nats_server_id="NDGERVW3RX7A6RAJQ34E7HPBFUD35322XRZJNTOMTFI7MHAXL2PS3OVW"} 0
nats_core_gateway_recv_msg_count{nats_server_cluster="region1",nats_server_gateway_id="9",nats_server_gateway_name="region2",nats_server_host="localhost",nats_server_id="NDYW2PLO6QVP2VKKUMWGWJXBMPTZKB3UAYME26BTKOGLNN55NSEK3RQF"} 1
nats_core_gateway_recv_msg_count{nats_server_cluster="region2",nats_server_gateway_id="4",nats_server_gateway_name="region1",nats_server_host="localhost",nats_server_id="NCBI75V5ASPJAEAR3VPS2YELXP7K6CUXXWAD5PB2SJ4BOIYQHU6JKV7A"} 5
# HELP nats_core_gateway_sent_bytes Number of messages sent over the gateway gauge
# TYPE nats_core_gateway_sent_bytes gauge
nats_core_gateway_sent_bytes{nats_server_cluster="region1",nats_server_gateway_id="7",nats_server_gateway_name="region2",nats_server_host="localhost",nats_server_id="NDGERVW3RX7A6RAJQ34E7HPBFUD35322XRZJNTOMTFI7MHAXL2PS3OVW"} 1719
nats_core_gateway_sent_bytes{nats_server_cluster="region1",nats_server_gateway_id="9",nats_server_gateway_name="region2",nats_server_host="localhost",nats_server_id="NDYW2PLO6QVP2VKKUMWGWJXBMPTZKB3UAYME26BTKOGLNN55NSEK3RQF"} 2286
nats_core_gateway_sent_bytes{nats_server_cluster="region2",nats_server_gateway_id="4",nats_server_gateway_name="region1",nats_server_host="localhost",nats_server_id="NCBI75V5ASPJAEAR3VPS2YELXP7K6CUXXWAD5PB2SJ4BOIYQHU6JKV7A"} 852
# HELP nats_core_gateway_sent_msgs Number of messages sent over the gateway gauge
# TYPE nats_core_gateway_sent_msgs gauge
nats_core_gateway_sent_msgs{nats_server_cluster="region1",nats_server_gateway_id="7",nats_server_gateway_name="region2",nats_server_host="localhost",nats_server_id="NDGERVW3RX7A6RAJQ34E7HPBFUD35322XRZJNTOMTFI7MHAXL2PS3OVW"} 2
nats_core_gateway_sent_msgs{nats_server_cluster="region1",nats_server_gateway_id="9",nats_server_gateway_name="region2",nats_server_host="localhost",nats_server_id="NDYW2PLO6QVP2VKKUMWGWJXBMPTZKB3UAYME26BTKOGLNN55NSEK3RQF"} 3
nats_core_gateway_sent_msgs{nats_server_cluster="region2",nats_server_gateway_id="4",nats_server_gateway_name="region1",nats_server_host="localhost",nats_server_id="NCBI75V5ASPJAEAR3VPS2YELXP7K6CUXXWAD5PB2SJ4BOIYQHU6JKV7A"} 1
# HELP nats_core_info General Server information Summary gauge
# TYPE nats_core_info gauge
nats_core_info{nats_server_cluster="region1",nats_server_host="localhost",nats_server_id="NDGERVW3RX7A6RAJQ34E7HPBFUD35322XRZJNTOMTFI7MHAXL2PS3OVW",nats_server_version="2.0.2"} 1
nats_core_info{nats_server_cluster="region1",nats_server_host="localhost",nats_server_id="NDYW2PLO6QVP2VKKUMWGWJXBMPTZKB3UAYME26BTKOGLNN55NSEK3RQF",nats_server_version="2.0.2"} 1
nats_core_info{nats_server_cluster="region2",nats_server_host="localhost",nats_server_id="NCBI75V5ASPJAEAR3VPS2YELXP7K6CUXXWAD5PB2SJ4BOIYQHU6JKV7A",nats_server_version="2.0.2"} 1
# HELP nats_core_mem_bytes Server memory gauge
# TYPE nats_core_mem_bytes gauge
nats_core_mem_bytes{nats_server_cluster="region1",nats_server_host="localhost",nats_server_id="NDGERVW3RX7A6RAJQ34E7HPBFUD35322XRZJNTOMTFI7MHAXL2PS3OVW"} 1.2685312e+07
nats_core_mem_bytes{nats_server_cluster="region1",nats_server_host="localhost",nats_server_id="NDYW2PLO6QVP2VKKUMWGWJXBMPTZKB3UAYME26BTKOGLNN55NSEK3RQF"} 1.2992512e+07
nats_core_mem_bytes{nats_server_cluster="region2",nats_server_host="localhost",nats_server_id="NCBI75V5ASPJAEAR3VPS2YELXP7K6CUXXWAD5PB2SJ4BOIYQHU6JKV7A"} 1.1309056e+07
# HELP nats_core_nats_up 1 if connected to NATS, 0 otherwise.  A gauge.
# TYPE nats_core_nats_up gauge
nats_core_nats_up 1
# HELP nats_core_recv_bytes Number of messages received gauge
# TYPE nats_core_recv_bytes gauge
nats_core_recv_bytes{nats_server_cluster="region1",nats_server_host="localhost",nats_server_id="NDGERVW3RX7A6RAJQ34E7HPBFUD35322XRZJNTOMTFI7MHAXL2PS3OVW"} 0
nats_core_recv_bytes{nats_server_cluster="region1",nats_server_host="localhost",nats_server_id="NDYW2PLO6QVP2VKKUMWGWJXBMPTZKB3UAYME26BTKOGLNN55NSEK3RQF"} 6528
nats_core_recv_bytes{nats_server_cluster="region2",nats_server_host="localhost",nats_server_id="NCBI75V5ASPJAEAR3VPS2YELXP7K6CUXXWAD5PB2SJ4BOIYQHU6JKV7A"} 4005
# HELP nats_core_recv_msgs_count Number of messages received gauge
# TYPE nats_core_recv_msgs_count gauge
nats_core_recv_msgs_count{nats_server_cluster="region1",nats_server_host="localhost",nats_server_id="NDGERVW3RX7A6RAJQ34E7HPBFUD35322XRZJNTOMTFI7MHAXL2PS3OVW"} 7
nats_core_recv_msgs_count{nats_server_cluster="region1",nats_server_host="localhost",nats_server_id="NDYW2PLO6QVP2VKKUMWGWJXBMPTZKB3UAYME26BTKOGLNN55NSEK3RQF"} 15
nats_core_recv_msgs_count{nats_server_cluster="region2",nats_server_host="localhost",nats_server_id="NCBI75V5ASPJAEAR3VPS2YELXP7K6CUXXWAD5PB2SJ4BOIYQHU6JKV7A"} 5
# HELP nats_core_route_pending_bytes Number of bytes pending in the route gauge
# TYPE nats_core_route_pending_bytes gauge
nats_core_route_pending_bytes{nats_server_cluster="region1",nats_server_host="localhost",nats_server_id="NDGERVW3RX7A6RAJQ34E7HPBFUD35322XRZJNTOMTFI7MHAXL2PS3OVW",nats_server_route_id="4"} 0
nats_core_route_pending_bytes{nats_server_cluster="region1",nats_server_host="localhost",nats_server_id="NDYW2PLO6QVP2VKKUMWGWJXBMPTZKB3UAYME26BTKOGLNN55NSEK3RQF",nats_server_route_id="4"} 0
# HELP nats_core_route_recv_bytes Number of bytes received over the route gauge
# TYPE nats_core_route_recv_bytes gauge
nats_core_route_recv_bytes{nats_server_cluster="region1",nats_server_host="localhost",nats_server_id="NDGERVW3RX7A6RAJQ34E7HPBFUD35322XRZJNTOMTFI7MHAXL2PS3OVW",nats_server_route_id="4"} 0
nats_core_route_recv_bytes{nats_server_cluster="region1",nats_server_host="localhost",nats_server_id="NDYW2PLO6QVP2VKKUMWGWJXBMPTZKB3UAYME26BTKOGLNN55NSEK3RQF",nats_server_route_id="4"} 5676
# HELP nats_core_route_recv_msg_count Number of messages received over the route gauge
# TYPE nats_core_route_recv_msg_count gauge
nats_core_route_recv_msg_count{nats_server_cluster="region1",nats_server_host="localhost",nats_server_id="NDGERVW3RX7A6RAJQ34E7HPBFUD35322XRZJNTOMTFI7MHAXL2PS3OVW",nats_server_route_id="4"} 7
nats_core_route_recv_msg_count{nats_server_cluster="region1",nats_server_host="localhost",nats_server_id="NDYW2PLO6QVP2VKKUMWGWJXBMPTZKB3UAYME26BTKOGLNN55NSEK3RQF",nats_server_route_id="4"} 7
# HELP nats_core_route_sent_bytes Number of bytes sent over the route gauge
# TYPE nats_core_route_sent_bytes gauge
nats_core_route_sent_bytes{nats_server_cluster="region1",nats_server_host="localhost",nats_server_id="NDGERVW3RX7A6RAJQ34E7HPBFUD35322XRZJNTOMTFI7MHAXL2PS3OVW",nats_server_route_id="4"} 5676
nats_core_route_sent_bytes{nats_server_cluster="region1",nats_server_host="localhost",nats_server_id="NDYW2PLO6QVP2VKKUMWGWJXBMPTZKB3UAYME26BTKOGLNN55NSEK3RQF",nats_server_route_id="4"} 0
# HELP nats_core_route_sent_msg_count Number of messages sent over the route gauge
# TYPE nats_core_route_sent_msg_count gauge
nats_core_route_sent_msg_count{nats_server_cluster="region1",nats_server_host="localhost",nats_server_id="NDGERVW3RX7A6RAJQ34E7HPBFUD35322XRZJNTOMTFI7MHAXL2PS3OVW",nats_server_route_id="4"} 7
nats_core_route_sent_msg_count{nats_server_cluster="region1",nats_server_host="localhost",nats_server_id="NDYW2PLO6QVP2VKKUMWGWJXBMPTZKB3UAYME26BTKOGLNN55NSEK3RQF",nats_server_route_id="4"} 7
# HELP nats_core_rtt_nanoseconds RTT in nanoseconds gauge
# TYPE nats_core_rtt_nanoseconds gauge
nats_core_rtt_nanoseconds{nats_server_cluster="region1",nats_server_host="localhost",nats_server_id="NDGERVW3RX7A6RAJQ34E7HPBFUD35322XRZJNTOMTFI7MHAXL2PS3OVW"} 1.8008293e+07
nats_core_rtt_nanoseconds{nats_server_cluster="region1",nats_server_host="localhost",nats_server_id="NDYW2PLO6QVP2VKKUMWGWJXBMPTZKB3UAYME26BTKOGLNN55NSEK3RQF"} 1.3031788e+07
nats_core_rtt_nanoseconds{nats_server_cluster="region2",nats_server_host="localhost",nats_server_id="NCBI75V5ASPJAEAR3VPS2YELXP7K6CUXXWAD5PB2SJ4BOIYQHU6JKV7A"} 1.7976382e+07
# HELP nats_core_sent_bytes Number of messages sent gauge
# TYPE nats_core_sent_bytes gauge
nats_core_sent_bytes{nats_server_cluster="region1",nats_server_host="localhost",nats_server_id="NDGERVW3RX7A6RAJQ34E7HPBFUD35322XRZJNTOMTFI7MHAXL2PS3OVW"} 7395
nats_core_sent_bytes{nats_server_cluster="region1",nats_server_host="localhost",nats_server_id="NDYW2PLO6QVP2VKKUMWGWJXBMPTZKB3UAYME26BTKOGLNN55NSEK3RQF"} 13661
nats_core_sent_bytes{nats_server_cluster="region2",nats_server_host="localhost",nats_server_id="NCBI75V5ASPJAEAR3VPS2YELXP7K6CUXXWAD5PB2SJ4BOIYQHU6JKV7A"} 852
# HELP nats_core_sent_msgs_count Number of messages sent gauge
# TYPE nats_core_sent_msgs_count gauge
nats_core_sent_msgs_count{nats_server_cluster="region1",nats_server_host="localhost",nats_server_id="NDGERVW3RX7A6RAJQ34E7HPBFUD35322XRZJNTOMTFI7MHAXL2PS3OVW"} 17
nats_core_sent_msgs_count{nats_server_cluster="region1",nats_server_host="localhost",nats_server_id="NDYW2PLO6QVP2VKKUMWGWJXBMPTZKB3UAYME26BTKOGLNN55NSEK3RQF"} 32
nats_core_sent_msgs_count{nats_server_cluster="region2",nats_server_host="localhost",nats_server_id="NCBI75V5ASPJAEAR3VPS2YELXP7K6CUXXWAD5PB2SJ4BOIYQHU6JKV7A"} 2
# HELP nats_core_slow_consumer_count Number of slow consumers gauge
# TYPE nats_core_slow_consumer_count gauge
nats_core_slow_consumer_count{nats_server_cluster="region1",nats_server_host="localhost",nats_server_id="NDGERVW3RX7A6RAJQ34E7HPBFUD35322XRZJNTOMTFI7MHAXL2PS3OVW"} 0
nats_core_slow_consumer_count{nats_server_cluster="region1",nats_server_host="localhost",nats_server_id="NDYW2PLO6QVP2VKKUMWGWJXBMPTZKB3UAYME26BTKOGLNN55NSEK3RQF"} 0
nats_core_slow_consumer_count{nats_server_cluster="region2",nats_server_host="localhost",nats_server_id="NCBI75V5ASPJAEAR3VPS2YELXP7K6CUXXWAD5PB2SJ4BOIYQHU6JKV7A"} 0
# HELP nats_core_start_time Server start time gauge
# TYPE nats_core_start_time gauge
nats_core_start_time{nats_server_cluster="region1",nats_server_host="localhost",nats_server_id="NDGERVW3RX7A6RAJQ34E7HPBFUD35322XRZJNTOMTFI7MHAXL2PS3OVW"} 1.571110522019796e+18
nats_core_start_time{nats_server_cluster="region1",nats_server_host="localhost",nats_server_id="NDYW2PLO6QVP2VKKUMWGWJXBMPTZKB3UAYME26BTKOGLNN55NSEK3RQF"} 1.571110522019795e+18
nats_core_start_time{nats_server_cluster="region2",nats_server_host="localhost",nats_server_id="NCBI75V5ASPJAEAR3VPS2YELXP7K6CUXXWAD5PB2SJ4BOIYQHU6JKV7A"} 1.571110952301371e+18
# HELP nats_core_subs_count Current number of subscriptions gauge
# TYPE nats_core_subs_count gauge
nats_core_subs_count{nats_server_cluster="region1",nats_server_host="localhost",nats_server_id="NDGERVW3RX7A6RAJQ34E7HPBFUD35322XRZJNTOMTFI7MHAXL2PS3OVW"} 17
nats_core_subs_count{nats_server_cluster="region1",nats_server_host="localhost",nats_server_id="NDYW2PLO6QVP2VKKUMWGWJXBMPTZKB3UAYME26BTKOGLNN55NSEK3RQF"} 17
nats_core_subs_count{nats_server_cluster="region2",nats_server_host="localhost",nats_server_id="NCBI75V5ASPJAEAR3VPS2YELXP7K6CUXXWAD5PB2SJ4BOIYQHU6JKV7A"} 8
# HELP nats_core_total_connection_count Total number of client connections serviced gauge
# TYPE nats_core_total_connection_count gauge
nats_core_total_connection_count{nats_server_cluster="region1",nats_server_host="localhost",nats_server_id="NDGERVW3RX7A6RAJQ34E7HPBFUD35322XRZJNTOMTFI7MHAXL2PS3OVW"} 2
nats_core_total_connection_count{nats_server_cluster="region1",nats_server_host="localhost",nats_server_id="NDYW2PLO6QVP2VKKUMWGWJXBMPTZKB3UAYME26BTKOGLNN55NSEK3RQF"} 5
nats_core_total_connection_count{nats_server_cluster="region2",nats_server_host="localhost",nats_server_id="NCBI75V5ASPJAEAR3VPS2YELXP7K6CUXXWAD5PB2SJ4BOIYQHU6JKV7A"} 0
```

Note, as this is not the recommended Prometheus archtecture, and in fact considered an
anti-pattern, we won't be submitting it to Prometheus as an official exporter at this time.

## TODO

- [X] Tests
- [X] TLS
- [X] Basic auth
- [X] Up/Down
- [X] Standardized Metric names
- [X] Docker Image
- [ ] Copy to nats-io
- [ ] CI
