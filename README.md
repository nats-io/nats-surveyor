[![License][License-Image]][License-Url] [![Build][Build-Status-Image]][Build-Status-Url] [![Coverage][Coverage-Image]][Coverage-Url]

# NATS Surveyor

NATS Monitoring, Simplified.

NATS surveyor polls the NATS server for `Statz` messages to generate data for
Prometheus.  This allows a single exporter to connect to any NATS server and
get an entire picture of a NATS deployment without requiring extra monitoring
components or sidecars.  Surveyor has been used extensively by [Synadia](https://synadia.com).

[System accounts](https://docs.nats.io/nats-server/configuration/sys_accounts)
must be enabled to use surveyor.

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
    	Credentials File for the system account when using jwt authentication
  -nkey string
      NKey Seed File for the system account when using nkey authenication
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

We feel Prometheus is the right fit for this project, but it's worth noting
that this is not the recommended Prometheus archtecture, preferring ease of use
over installing and configuring a full monitoring infrastructure.  For a more
robust monitoring architecture the
[prometheus-nats-exporter](https://github.com/nats-io/prometheus-nats-exporter)
should be placed and configured alongside every NATS server component.

## Docker Compose

An easy way to start the NATS Surveyor stack (Grafana, Prometheus, and NATS
Surveyor) is through docker-compose.

Follow these links for installation instructions:

* [Docker Installation](https://docs.docker.com/v17.09/engine/installation/)
* [Docker Compose Installation](https://docs.docker.com/compose/install/)

### Environment Variables

The following environment variables MUST be set, either in your environment or
through the [.env](./docker-compose/.env) file that is automatically read by
docker-compose.  There is a `survey.sh` script that will set them for you as
a convenience.

| Environment Variable | Example | Description |
|--|--|--|
| NATS_SURVEYOR_SERVERS | nats://hostname:4222 | The URLs of any deployed NATS server(s) |
| NATS_SURVEYOR_CREDS   | ./SYS.creds          | NATS 2.0 System Account credentials |
| NATS_SURVEYOR_SERVER_COUNT | 9               | Number of expected NATS servers |
| PROMETHEUS_STORAGE    | ./storage/prometheus | Path to store prometheus data locally |
| SURVEYOR_DOCKER_TAG   | latest               | Surveyor docker tag to pull |
| PROMETHEUS_DOCKER_TAG | latest               | Prometheus docker tag to pull |
| GRAFANA_DOCKER_TAG    | latest               | Grafana docker tag to pull |

Note:  For referencing files and paths, docker always expects volume mounts
to be either a fully qualified directory, or a relative directory beginning
with with `./`.

#### Server URLs

You only need to connect to a single NATS server to monitor your entire NATS
deployment.  In configuring NATS_SURVEYOR_SERVERS, only one server is required,
but it's recommended you provide a list for backup servers to connect to, e.g.
`nats://host1:4222,nats://host2:5222`.  Valid urls are formatted as `hostname`
(defaulting to port 4222), `hostname:port`, or `nats://hostname:port`.

### Starting Up

You can start the Surveyor stack two ways.  The first is through docker
compose.  Ensure the environment varibles are set, that you are working
from the /docker-compose directory and run `docker-compose up`.

```bash
$ docker-compose up
Recreating nats-surveyor ... done
Recreating prometheus    ... done
Recreating grafana       ... done
Attaching to nats-surveyor, prometheus, grafana
...
```

Alternatively, you can pass variables into the `survey.sh` script in the
docker-compose directory.

```bash
$ ./survey.sh
usage: survey.sh <url> <server count> <system credentials>
```

e.g.

`./survey.sh  nats://mydeployment:4222 24 /privatekeys/SYS.creds`

If things aren't working, look in the output for any lines that contain
`exited with code 1` and address the problem. They are usually docker
volume mount problems or connectivity problems.

Next, with your browser, navigate to `http://127.0.0.1:3000`, or if you are
running the Surveyor stack remotely, the hostname of the host running the
NATS surveyor stack, e.g. `http://yourremotehost:3000`.

The first time you connect, you'll need to login:

* User:  *admin*
* Password: *admin*

After logging in, navigate to "Manage dashboards" and you'll see a dashboard
available named **NATS Surveyor**, where you'll be able to monitor your
entire NATS deployment.

### Stopping (while keeping the containers)

To stop the surveyor stack, but keep the containers run: `docker-compose stop`

### Restarting Surveyor

To restart the surveyor stack after being stopped, run: `docker-compose up`

### Stopping and removing containers

To cleanup your installation, run: `docker-compose down`

### Running Surveyor as a service

For platforms that support `systemd`, [surveyor.service](./docker-compose/surveyor.service)
is provided as a service definition template.  Modify and save this file as
`/etc/systemd/system/surveyor.service`.

`systemctl start surveyor` will launch the service.

### Errors

The logs should normally contain enough information about the cause of
problems or errors.

If you encounter a Prometheus error of:
`panic: Unable to create mmap-ed active query log`, set the UID of the
container to match the UID of your user in the
[docker-compose](./docker-compose/docker-compose.yml) file.

e.g:

```yaml
  prometheus:
    image: prom/prometheus:${PROMETHEUS_DOCKER_TAG}
    user: "1000:1000"
```

If the above doesn't work, using `root` will work but may pose a
security thread to the node it is running on.

```yaml
  prometheus:
    image: prom/prometheus:${PROMETHEUS_DOCKER_TAG}
    user: root
```

More information can be found [here](https://github.com/prometheus/prometheus/issues/5976).

## Service Observations

Services can be observed by creating JSON files in the `observations` directory.
Both jwt credential files and nkey seed files are supported. The name of the observation has to unique. A second observation with a duplicate name will be ignored.

Here's an example using a jwt credential file:

```json
{
  "name": "email.subscribe",
  "topic": "monitor.email.subscribe",
  "credential": "/observations/email.subscribe.cred"
}
```
Example with nkey seed file:
```json
{
  "name": "email.subscribe",
  "topic": "monitor.email.subscribe",
  "nkey": "/observations/email.subscribe.nkey"
}
```

Place this in `observations/email.surbscribe.json` and create a credential giving access to this topic in `observations/email.subscribe.cred`, when you restart the service any observations published by the NATS system will be tracked and graphed.

## JetStream

JetStream can be monitored on a per-account basis by creating JSON files in the `jetstream` directory.
Place those files in `jetstream/youraccount.json`. Be sure that you give access to the `$JS.EVENT.>` subject to your user.

When you add/modify account files, you'll need restart the NATS Surveyor service in order for the JetStream in this account to be monitored.

There are some ways to establish authentication, here are some examples:

### Credentials
```json
{
  "name": "Your Account",
  "credential": "/jetstream/youraccount.cred"
}
```

### User/Password
```json
{
  "name": "Your Account",
  "username": "accounta",
  "password": "changeit"
}
```

### NKeys
```json
{
  "name": "Your Account",
  "nkey": "UDXU4RCSJNZOIQHZNWXHXORDPRTGNJAHAHFRGZNEEJCPQTT2M7NLCNF4"
}
```
### mTLS

```json
{
  "name": "Your Account",
  "tls_ca": "/etc/nats-certs/your-account/ca.crt",
  "tls_cert": "/etc/nats-certs/your-account/tls.crt",
  "tls_key": "/etc/nats-certs/your-account/tls.key"
}
```

## TODO

- [ ] Windows builds
- [ ] Other events (connections, disconnects, etc)
- [ ] Best Guess Server Count

[License-Url]: https://www.apache.org/licenses/LICENSE-2.0
[License-Image]: https://img.shields.io/badge/License-Apache2-blue.svg
[Build-Status-Image]: https://github.com/nats-io/nats-surveyor/workflows/Testing/badge.svg
[Build-Status-Url]: https://github.com/nats-io/nats-surveyor/workflows/Testing/badge.svg
[Coverage-Url]: https://codecov.io/gh/nats-io/nats-surveyor
[Coverage-image]: https://codecov.io/gh/nats-io/nats-surveyor/branch/main/graph/badge.svg
