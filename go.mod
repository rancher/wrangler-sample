module github.com/rancher/wrangler-sample

go 1.16

replace k8s.io/client-go => k8s.io/client-go v0.18.0

require (
	github.com/rancher/lasso v0.0.0-20210616224652-fc3ebd901c08
	github.com/rancher/wrangler v0.8.10
	github.com/rancher/wrangler-api v0.6.0
	github.com/sirupsen/logrus v1.4.2
	k8s.io/api v0.18.8
	k8s.io/apimachinery v0.18.8
	k8s.io/client-go v11.0.1-0.20190409021438-1a26190bd76a+incompatible
)

replace github.com/rancher/wrangler-api v0.6.0 => github.com/dylanhitt/wrangler-api v0.7.0
