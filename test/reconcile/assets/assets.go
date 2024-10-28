package assets

import (
	"embed"
	hwmgrpluginoranopenshiftiov1alpha1 "github.com/openshift-kni/oran-hwmgr-plugin/api/hwmgr-plugin/v1alpha1"
	imsv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

var (

	//go:embed manifests/*
	manifests embed.FS

	sch  = runtime.NewScheme()
	cdcs = serializer.NewCodecFactory(sch)
)

func InitCodecs() error {
	if err := corev1.AddToScheme(sch); nil != err {
		return err
	}
	if err := hwmgrpluginoranopenshiftiov1alpha1.AddToScheme(sch); nil != err {
		return err
	}
	if err := imsv1alpha1.AddToScheme(sch); nil != err {
		return err
	}
	return nil
}

func GetConfigmapFromFile(name string) (*corev1.ConfigMap, error) {
	configmapBytes, err := manifests.ReadFile(name)
	if err != nil {
		return nil, err
	}

	configmapObject, err := runtime.Decode(cdcs.UniversalDecoder(corev1.SchemeGroupVersion), configmapBytes)
	if err != nil {
		return nil, err
	}

	return configmapObject.(*corev1.ConfigMap), nil
}

func GetHardwareManageFromFile(name string) (*hwmgrpluginoranopenshiftiov1alpha1.HardwareManager, error) {
	hardwaremgrBytes, err := manifests.ReadFile(name)
	if err != nil {
		return nil, err
	}

	hardwaremgrObject, err := runtime.Decode(cdcs.UniversalDecoder(hwmgrpluginoranopenshiftiov1alpha1.GroupVersion), hardwaremgrBytes)
	if err != nil {
		return nil, err
	}

	return hardwaremgrObject.(*hwmgrpluginoranopenshiftiov1alpha1.HardwareManager), nil
}

func GetNodePoolFromFile(name string) (*imsv1alpha1.NodePool, error) {
	nodepoolBytes, err := manifests.ReadFile(name)
	if err != nil {
		return nil, err
	}

	nodepoolObject, err := runtime.Decode(cdcs.UniversalDecoder(imsv1alpha1.GroupVersion), nodepoolBytes)
	if err != nil {
		return nil, err
	}

	return nodepoolObject.(*imsv1alpha1.NodePool), nil
}
