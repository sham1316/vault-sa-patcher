package vault

import (
	"context"
	b64 "encoding/base64"
	"fmt"
	vault "github.com/hashicorp/vault/api"
	auth "github.com/hashicorp/vault/api/auth/kubernetes"
	"go.uber.org/zap"
	"strings"
	"sync"
	"time"
	"vault-sa-patcher/config"
	"vault-sa-patcher/internal/k8s"
)

type ImagePullSecret struct {
	host      string
	username  string
	password  string
	kubSecret []byte
}
type ImagePullSecretMap map[string]*ImagePullSecret

type Service interface {
	FetchImagePullSecret(ctx context.Context)
	GetImagePullSecrets() map[string][]byte
}

type vaultService struct {
	cfg                *config.Config
	token              string
	ca                 []byte
	imagePullSecretMap ImagePullSecretMap
	sync.Mutex
}

func New(cfg *config.Config, ks k8s.KubeService) Service {
	return &vaultService{
		cfg:   cfg,
		token: ks.GetToken(),
		ca:    ks.GetCA(),
	}
}
func (vs *vaultService) FetchImagePullSecret(ctx context.Context) {
	config := vault.DefaultConfig()
	config.Address = fmt.Sprintf("%s://%s:%d", vs.cfg.Vault.VaultSchema, vs.cfg.Vault.VaultServer, vs.cfg.Vault.VaultPort)
	config.Timeout = 60 * time.Second
	client, err := vault.NewClient(config)

	if err != nil {
		zap.S().Error(err)
	}
	k8sAuth, err := auth.NewKubernetesAuth(
		vs.cfg.Vault.VaultRole,
		auth.WithServiceAccountToken(vs.token))
	if vs.cfg.InCluster {
		k8sAuth, err = auth.NewKubernetesAuth(
			vs.cfg.Vault.VaultRole,
			auth.WithServiceAccountTokenPath(vs.cfg.TokenPath))
	}

	if err != nil {
		zap.S().Errorf("unable to initialize Kubernetes auth method: %v", err)
		return
	}

	authInfo, err := client.Auth().Login(ctx, k8sAuth)
	if err != nil {
		zap.S().Errorf("unable to log in with Kubernetes auth: %v", err)
		return
	}
	if authInfo == nil {
		zap.S().Errorf("no auth info was returned after login")
		return
	}

	secret, err := client.KVv2(vs.cfg.Vault.MountPath).Get(context.Background(), vs.cfg.Vault.SecretPath)
	if err != nil {
		zap.S().Errorf("unable to read secret: %v", err)
		return
	}
	vs.Lock()
	defer vs.Unlock()

	vs.imagePullSecretMap = make(ImagePullSecretMap)
	for key, value := range secret.Data {
		_key := strings.Split(key, "/")
		if _, ok := vs.imagePullSecretMap[_key[0]]; !ok {
			vs.imagePullSecretMap[_key[0]] = &ImagePullSecret{}
		}
		if _key[1] == "host" {
			vs.imagePullSecretMap[_key[0]].host = value.(string)
		}
		if _key[1] == "username" {
			vs.imagePullSecretMap[_key[0]].username = value.(string)
		}
		if _key[1] == "password" {
			vs.imagePullSecretMap[_key[0]].password = value.(string)
		}
	}
	for key, value := range vs.imagePullSecretMap {
		zap.S().Infof("imagePullSecret: %s(%s/%s***@%s)", key, value.username, value.password[0:1], value.host)
		base64Auth := make([]byte, b64.StdEncoding.EncodedLen(len(value.username+":"+value.password)))
		b64.StdEncoding.Encode(base64Auth, []byte(value.username+":"+value.password))
		kubSecret := fmt.Sprintf(`{"auths": {"%s": {"auth": "%s"}}}`, value.host, base64Auth)
		value.kubSecret = []byte(kubSecret)
	}
}

func (vs *vaultService) GetImagePullSecrets() map[string][]byte {
	vs.Lock()
	defer vs.Unlock()
	m := make(map[string][]byte)
	for key, value := range vs.imagePullSecretMap {
		m[key] = make([]byte, len(value.kubSecret))
		copy(m[key], value.kubSecret)
	}
	return m
}
