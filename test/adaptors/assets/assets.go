/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

//nolint:all
package assets

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"

	hwmgrpluginoranopenshiftiov1alpha1 "github.com/openshift-kni/oran-hwmgr-plugin/api/hwmgr-plugin/v1alpha1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
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
	if err := corev1.AddToScheme(sch); err != nil {
		return fmt.Errorf("%s failed with error: (%w)", "corev1", err)
	}
	if err := hwmgrpluginoranopenshiftiov1alpha1.AddToScheme(sch); err != nil {
		return fmt.Errorf("%s failed with error: (%w)", "hwmgrv1alpha1", err)
	}
	if err := hwmgmtv1alpha1.AddToScheme(sch); err != nil {
		return fmt.Errorf("%s failed with error: (%w)", "hwmgmtv1alpha1", err)
	}
	return nil
}

func GetConfigmapFromFile(name string) (*corev1.ConfigMap, error) {
	configmapBytes, err := manifests.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("%s failed with error: (%w)", "readfile", err)
	}

	configmapObject, err := runtime.Decode(cdcs.UniversalDecoder(corev1.SchemeGroupVersion), configmapBytes)
	if err != nil {
		return nil, fmt.Errorf("%s failed with error: (%w)", "decode", err)
	}

	return configmapObject.(*corev1.ConfigMap), nil
}

func GetHardwareManagerFromTmpl(url string, tmpl string) (*hwmgrpluginoranopenshiftiov1alpha1.HardwareManager, error) {

	t := template.Must(template.New("dell-hwmgr.tmpl").ParseFS(manifests, tmpl))
	var data struct {
		Url string
	}
	data.Url = url

	var hardwaremgrBuffer bytes.Buffer
	err := t.Execute(&hardwaremgrBuffer, data)
	if err != nil {
		return nil, fmt.Errorf("failed to execute template %s with error: (%w)", tmpl, err)
	}

	hardwaremgrBytes := hardwaremgrBuffer.Bytes()
	hardwaremgrObject, err := runtime.Decode(cdcs.UniversalDecoder(hwmgrpluginoranopenshiftiov1alpha1.GroupVersion), hardwaremgrBytes)
	if err != nil {
		return nil, fmt.Errorf("%s failed with error: (%w)", "decode", err)
	}

	return hardwaremgrObject.(*hwmgrpluginoranopenshiftiov1alpha1.HardwareManager), nil
}

func GetHardwareManagerFromFile(name string) (*hwmgrpluginoranopenshiftiov1alpha1.HardwareManager, error) {
	hardwaremgrBytes, err := manifests.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("%s failed with error: (%w)", "readfile", err)
	}

	hardwaremgrObject, err := runtime.Decode(cdcs.UniversalDecoder(hwmgrpluginoranopenshiftiov1alpha1.GroupVersion), hardwaremgrBytes)
	if err != nil {
		return nil, fmt.Errorf("%s failed with error: (%w)", "decode", err)
	}

	return hardwaremgrObject.(*hwmgrpluginoranopenshiftiov1alpha1.HardwareManager), nil
}

func GetNodePoolFromFile(name string) (*hwmgmtv1alpha1.NodePool, error) {
	nodepoolBytes, err := manifests.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("%s failed with error: (%w)", "readfile", err)
	}

	nodepoolObject, err := runtime.Decode(cdcs.UniversalDecoder(hwmgmtv1alpha1.GroupVersion), nodepoolBytes)
	if err != nil {
		return nil, fmt.Errorf("%s failed with error: (%w)", "decode", err)
	}

	return nodepoolObject.(*hwmgmtv1alpha1.NodePool), nil
}

func GetNameSpaceFromFile(name string) (*corev1.Namespace, error) {
	namespaceBytes, err := manifests.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("%s failed with error: (%w)", "readfile", err)
	}

	namespaceObject, err := runtime.Decode(cdcs.UniversalDecoder(corev1.SchemeGroupVersion), namespaceBytes)
	if err != nil {
		return nil, fmt.Errorf("%s failed with error: (%w)", "decode", err)
	}

	return namespaceObject.(*corev1.Namespace), nil
}

func GetSecretFromFile(name string) (*corev1.Secret, error) {
	secretBytes, err := manifests.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("%s failed with error: (%w)", "readfile", err)
	}

	secretObject, err := runtime.Decode(cdcs.UniversalDecoder(corev1.SchemeGroupVersion), secretBytes)
	if err != nil {
		return nil, fmt.Errorf("%s failed with error: (%w)", "decode", err)
	}

	return secretObject.(*corev1.Secret), nil
}
