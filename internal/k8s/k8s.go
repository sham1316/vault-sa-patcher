package k8s

import (
	"context"
	"flag"
	"go.uber.org/zap"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"path/filepath"
	"sync"
	"vault-sa-patcher/config"
)

type KubeService interface {
	CreateImagePullSecret(name, ns string, data []byte) *v1.Secret
	SaveImagePullSecret(ctx context.Context, secret *v1.Secret) *v1.Secret
	UpdateImagePullSecret(ctx context.Context, secret *v1.Secret) *v1.Secret
	GetNamespaceList(ctx context.Context) *v1.NamespaceList
	GetServiceAccountList(ctx context.Context, ns string) *v1.ServiceAccountList
	UpdateServiceAccount(ctx context.Context, sa *v1.ServiceAccount) *v1.ServiceAccount
	GetSecretList(ctx context.Context, ns string) *v1.SecretList
	GetImagePullSecret(ctx context.Context, name, ns string) *v1.Secret
	DeleteSecret(ctx context.Context, name, ns string)
	GetToken() string
	GetCA() []byte
}

type kubeService struct {
	Cfg             *config.Config
	k8sConfig       *rest.Config
	clientSet       *kubernetes.Clientset
	resourceVersion string
	labelSelector   *metav1.LabelSelector
	sync.Mutex
}

func NewKubeService(cfg *config.Config) KubeService {
	k8sConfig := getConfig(cfg.InCluster, cfg.Kubeconfig)
	clientSet, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		zap.S().Fatal(err)
	}
	labelSelector := &metav1.LabelSelector{MatchLabels: map[string]string{config.Annotation + "/sync": "true"}}
	return &kubeService{
		Cfg:             cfg,
		k8sConfig:       k8sConfig,
		clientSet:       clientSet,
		labelSelector:   labelSelector,
		resourceVersion: "0",
	}
}

func getConfig(inCluster bool, kubeconfig string) *rest.Config {
	var config *rest.Config
	var err error
	if inCluster {
		config, err = rest.InClusterConfig()
	} else {
		if kubeconfig == "" {
			if home := homedir.HomeDir(); home != "" {
				kubeconfig = *flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
			} else {
				kubeconfig = *flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
			}
			flag.Parse()
		}
		// use the current context in kubeconfig
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	if err != nil {
		zap.S().Fatal(err)
	}
	return config
}

func (k *kubeService) GetNamespaceList(ctx context.Context) *v1.NamespaceList {
	k.Lock()
	defer k.Unlock()
	opt := metav1.ListOptions{}
	nsList, err := k.clientSet.CoreV1().Namespaces().List(ctx, opt)

	if err != nil {
		zap.S().Errorf("error: %v", err)
		return nil
	}
	return nsList
}

func (k *kubeService) GetServiceAccountList(ctx context.Context, ns string) *v1.ServiceAccountList {
	k.Lock()
	defer k.Unlock()
	opt := metav1.ListOptions{LabelSelector: labels.Set(k.labelSelector.MatchLabels).String()}

	saList, err := k.clientSet.CoreV1().ServiceAccounts(ns).List(ctx, opt)
	k.resourceVersion = saList.GetResourceVersion()

	if err != nil {
		zap.S().Errorf("error: %v", err)
		return nil
	}
	return saList
}

func (k *kubeService) DeleteSecret(ctx context.Context, name, ns string) {
	zap.S().Infof("%s(%s) - delete secret", name, ns)
	err := k.clientSet.CoreV1().Secrets(ns).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		zap.S().Warnf("error: %v", err)
	}
}

func (k *kubeService) GetSecretList(ctx context.Context, ns string) *v1.SecretList {
	k.Lock()
	defer k.Unlock()
	opt := metav1.ListOptions{LabelSelector: labels.Set(k.labelSelector.MatchLabels).String()}

	secretList, err := k.clientSet.CoreV1().Secrets(ns).List(ctx, opt)
	k.resourceVersion = secretList.GetResourceVersion()

	if err != nil {
		zap.S().Errorf("error: %v", err)
		return nil
	}
	return secretList
}
func (k *kubeService) GetToken() string {
	return k.k8sConfig.BearerToken
}

func (k *kubeService) GetCA() []byte {
	return k.k8sConfig.TLSClientConfig.CAData
}

func (k *kubeService) GetImagePullSecret(ctx context.Context, name, ns string) *v1.Secret {
	opt := metav1.GetOptions{}
	secret, err := k.clientSet.CoreV1().Secrets(ns).Get(ctx, name, opt)
	if err != nil {
		zap.S().Errorf("error: %v", err)
		return nil
	}
	return secret
}

func (k *kubeService) CreateImagePullSecret(name, ns string, data []byte) *v1.Secret {

	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   ns,
			Annotations: k.labelSelector.MatchLabels,
			Labels:      k.labelSelector.MatchLabels,
		},
		Type: v1.SecretTypeDockerConfigJson,
		Data: map[string][]byte{".dockerconfigjson": data},
	}
	return secret
}
func (k *kubeService) SaveImagePullSecret(ctx context.Context, secret *v1.Secret) *v1.Secret {
	zap.S().Debugf("%s(%s) create imagePullSecret", secret.Name, secret.Namespace)
	_secret, err := k.clientSet.CoreV1().Secrets(secret.Namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		zap.S().Errorf("error: %v", err)
		return nil
	}
	return _secret
}
func (k *kubeService) UpdateImagePullSecret(ctx context.Context, secret *v1.Secret) *v1.Secret {
	zap.S().Debugf("%s(%s) update imagePullSecret", secret.Name, secret.Namespace)
	_secret, err := k.clientSet.CoreV1().Secrets(secret.Namespace).Update(ctx, secret, metav1.UpdateOptions{})
	if err != nil {
		zap.S().Errorf("error: %v", err)
		return nil
	}
	return _secret
}

func (k *kubeService) UpdateServiceAccount(ctx context.Context, sa *v1.ServiceAccount) *v1.ServiceAccount {
	zap.S().Debugf("%s(%s) update serviceAccount", sa.Name, sa.Namespace)
	_sa, err := k.clientSet.CoreV1().ServiceAccounts(sa.Namespace).Update(ctx, sa, metav1.UpdateOptions{})
	if err != nil {
		zap.S().Errorf("error: %v", err)
		return nil
	}
	return _sa

}
