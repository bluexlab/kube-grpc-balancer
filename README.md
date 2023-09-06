# kube-grpc-balancer

GRPC load balancer for Kubernetes (gproxy) implements the client side grpc load balancing for the component which doesn't support grpc load balancing like golang implementation of grpc.

## Basic Concept

```
                    +-----------------------+
                    | Kubernetes API Server |
                    +-----------------------+
                           |
                           |              +--------+
                           |    +-------->| Server |
                           v    |         +--------+
    +--------+           +--------+
    | Client |---------->| gproxy |
    +--------+           +--------+
                                |
                                |         +--------+
                                +-------->| Server |
                                          +--------+
```

Instead of the client connecting to the servers directly, the client connects to the gproxy. The gproxy connects to the servers on behalf of the client. The gproxy uses the Kubernetes API to watch the servers and keep the connections to the servers. The gproxy also implements the gRPC load balancing policy to balance the load between the servers.
Current implementation supports the round robin load balancing policy.

## Resolver

gproxy supports three types of resolvers. Kubernetes resolver, DNS resolver and static resolver. The Kubernetes resolver uses the Kubernetes API to watch the servers. The DNS resolver uses the DNS to resolve the servers. The static resolver uses the static list of servers.

## Kubernetes Resolver

To use Kubernetes resolver, the proxy address should be in the following format.

```
    kubernetes:///<service-name>:<port>
```

The service name is the name of the Kubernetes service.

gproxy needs `GET` and `WATCH` access to the endpoints if you are using RBAC in your cluster.

## DNS Resolver

To use DNS resolver, the proxy address should be in the following format.

```
    dns:///<service-name>:<port>
```

## Command Line

```
Kubernetes gRPC Balancer


Flags:
  -h, --[no-]help          Show context-sensitive help (also try --help-long and --help-man).
      --[no-]version       Show application version.
  -p, --proxy=PROXY ...    The address and port of the proxy and the destination of the service. Eg: localhost:5000-kubernetes:///service:5000
  -d, --shutdown-delay=5s  The delay before shutting down the proxy.
```
