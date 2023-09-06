package main

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alecthomas/kingpin/v2"
	"github.com/bluexlab/kube-grpc-balancer/pkg/proxy"
	formatter "github.com/bluexlab/logrus-formatter"
	"github.com/sirupsen/logrus"
)

const (
	appName    = "kube-grpc-balancer"
	appDesc    = "Kubernetes gRPC Balancer"
	appVersion = "0.0.1"
)

type ProxyConfig struct {
	ProxyAddress   string
	ServiceAddress string
}

func ParseProxyConfig(proxy string) (ProxyConfig, error) {
	splitIdx := strings.Index(proxy, "-")
	if splitIdx <= 0 {
		return ProxyConfig{}, fmt.Errorf(`%q is invalid proxy configuration`, proxy)
	}

	cfg := ProxyConfig{
		ProxyAddress:   proxy[:splitIdx],
		ServiceAddress: proxy[splitIdx+1:],
	}
	if len(cfg.ProxyAddress) == 0 || len(cfg.ServiceAddress) == 0 {
		return ProxyConfig{}, fmt.Errorf(`%q is invalid proxy configuration`, proxy)
	}
	return cfg, nil
}

func main() {
	formatter.InitLogger()
	app := kingpin.New(appName, appDesc)
	app.Version(appVersion)
	app.HelpFlag.Short('h')

	proxyFlags := app.Flag("proxy", "The address and port of the proxy and the destination of the service. Eg: localhost:5000-kubernetes:///service:5000").Short('p').Required().Strings()
	shutdownDelayFlag := app.Flag("shutdown-delay", "The delay before shutting down the proxy.").Short('d').Default("5s").Duration()

	kingpin.MustParse(app.Parse(os.Args[1:]))

	proxyConfigs := make([]ProxyConfig, 0, len(*proxyFlags))
	for _, pf := range *proxyFlags {
		pc, err := ParseProxyConfig(pf)
		if err != nil {
			fmt.Printf("error: %s\n", err)
			os.Exit(1)
		}
		proxyConfigs = append(proxyConfigs, pc)
	}

	proxies := make([]*proxy.Proxy, 0, len(proxyConfigs))
	for _, pc := range proxyConfigs {
		p, err := proxy.NewProxy(pc.ProxyAddress, pc.ServiceAddress)
		if err != nil {
			fmt.Printf("error: %s\n", err)
			os.Exit(1)
		}
		proxies = append(proxies, p)
	}

	quit := make(chan os.Signal, 1+len(proxies))
	signal.Notify(quit, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	wg := sync.WaitGroup{}
	wg.Add(len(proxies))

	logrus.Info("Bring up proxies...")
	for i := range proxies {
		go func(p *proxy.Proxy) {
			defer wg.Done()
			if err := p.Serve(); err != nil {
				logrus.Errorf("error: %s\n", err)
				quit <- os.Interrupt
			}
		}(proxies[i])
	}
	logrus.Info("Ready.")

	<-quit
	// Hold on for a bit to allow the client to finish.
	if shutdownDelayFlag != nil {
		logrus.Info("Cooling down...")
		time.Sleep(*shutdownDelayFlag)
	}
	logrus.Info("Stopping...")
	for i := range proxies {
		proxies[i].Stop()
	}
	wg.Wait()
	logrus.Info("Bye.")
}
