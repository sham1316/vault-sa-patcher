package controller

import (
	"context"
	"go.uber.org/dig"
	"go.uber.org/zap"
	v1 "k8s.io/api/core/v1"
	"slices"
	"time"
	"vault-sa-patcher/config"
	"vault-sa-patcher/internal/k8s"
	"vault-sa-patcher/internal/vault"
)

type loopControllerParams struct {
	dig.In

	Cfg   *config.Config
	Ks    k8s.KubeService
	Vault vault.Service
}

type loopController struct {
	p loopControllerParams
}

func (l *loopController) Start(ctx context.Context) {
	go func() {
		zap.S().Info("loop started")
		l.updater(ctx)
		ticker := time.NewTicker(time.Second * time.Duration(l.p.Cfg.Interval))
		for {
			select {
			case <-ctx.Done():
				zap.S().Info("finish main context")
				return
			case t := <-ticker.C:
				zap.S().Info("start")
				l.updater(ctx)
				zap.S().Info("finish:", time.Since(t))
			}
		}
	}()
}

func NewLoopController(p loopControllerParams) Result {
	return Result{
		Controller: &loopController{
			p: p,
		},
	}
}

func (l *loopController) updater(ctx context.Context) {
	defer func() {
		if err := recover(); err != nil {
			zap.S().Errorw("recovered", err)
		}
	}()

	nsList := l.p.Ks.GetNamespaceList(ctx)
	for _, ns := range nsList.Items {
		isSecretNeeded := false
		saList := l.p.Ks.GetServiceAccountList(ctx, ns.Name)
		for _, sa := range saList.Items {
			if value, ok := sa.Annotations[config.Annotation+"/sync"]; ok && value == "true" {
				zap.S().Debugf("%s(%s) annotation %s - EXIST", sa.Name, sa.Namespace, config.Annotation)
				kubSecretNameList := l.createOrUpdateSecrets(ctx, ns.Name)
				l.updateServiceAccount(ctx, sa, kubSecretNameList)
				isSecretNeeded = true
			} else {
				zap.S().Debugf("%s(%s) no annotation %s - SKIP", sa.Name, sa.Namespace, config.Annotation)
			}
		}
		if !isSecretNeeded {
			zap.S().Debugf("namespace(%s) - no SA with annotation %s - delete secret", ns.Name, config.Annotation)
		}
	}
}

func (l *loopController) createOrUpdateSecrets(ctx context.Context, ns string) []string {
	vaultSecrets := l.p.Vault.GetImagePullSecrets()
	if len(vaultSecrets) == 0 {
		zap.S().Warnf("vault return empty ImagePullSecret")
	}
	var kubSecretNameList []string
	for key, secret := range vaultSecrets {
		kubSecretName := l.p.Cfg.SecretNamePrefix + key
		kubSecretNameList = append(kubSecretNameList, kubSecretName)
		existSecret := l.p.Ks.GetImagePullSecret(ctx, kubSecretName, ns)
		if existSecret == nil {
			newSecret := l.p.Ks.CreateImagePullSecret(kubSecretName, ns, secret)
			l.p.Ks.SaveImagePullSecret(ctx, newSecret)
			continue
		} else {
			if string(existSecret.Data[".dockerconfigjson"]) == string(secret) {
				zap.S().Debugf("%s(%s) - imagePullSecret not change", kubSecretName, ns)
			} else {
				existSecret.Data[".dockerconfigjson"] = secret
				l.p.Ks.UpdateImagePullSecret(ctx, existSecret)
			}
		}
	}
	return kubSecretNameList
}

func (l *loopController) updateServiceAccount(ctx context.Context, sa v1.ServiceAccount, kubSecretNameList []string) {
	var existImagePullSecrets []string
	for _, obj := range sa.ImagePullSecrets {
		existImagePullSecrets = append(existImagePullSecrets, obj.Name)
	}

	if slices.Equal(existImagePullSecrets, kubSecretNameList) {
		zap.S().Debugf("%s(%s) - serviceAccount not change", sa.Name, sa.Namespace)
		return
	}
	var newImagePullSecrets []v1.LocalObjectReference
	for _, name := range kubSecretNameList {
		newImagePullSecrets = append(newImagePullSecrets, v1.LocalObjectReference{Name: name})
	}
	sa.ImagePullSecrets = newImagePullSecrets
	l.p.Ks.UpdateServiceAccount(ctx, &sa)
}
