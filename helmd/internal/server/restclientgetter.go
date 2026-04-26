package server

import (
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
)

// restClientGetter satisfies genericclioptions.RESTClientGetter using an in-cluster rest.Config.
type restClientGetter struct {
	flags   *genericclioptions.ConfigFlags
	restCfg *rest.Config
}

func (r *restClientGetter) ToRESTConfig() (*rest.Config, error) {
	return r.restCfg, nil
}

func (r *restClientGetter) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
	dc, err := discovery.NewDiscoveryClientForConfig(r.restCfg)
	if err != nil {
		return nil, err
	}
	return memory.NewMemCacheClient(dc), nil
}

func (r *restClientGetter) ToRESTMapper() (restmapper.PriorityRESTMapper, error) {
	dc, err := r.ToDiscoveryClient()
	if err != nil {
		return restmapper.PriorityRESTMapper{}, err
	}
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(dc)
	expander := restmapper.NewShortcutExpander(mapper, dc, nil)
	return restmapper.PriorityRESTMapper{Delegate: expander}, nil
}

func (r *restClientGetter) ToRawKubeConfigLoader() clientcmd.ClientConfig {
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{},
	)
}
