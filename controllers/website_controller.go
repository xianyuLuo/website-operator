/*


Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/go-logr/logr"
	appsv1beta1 "k8s.io/api/apps/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"reflect"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	websitev1 "github.com/xianyuluo/website-operator/api/v1"
)

// WebsiteReconciler reconciles a Website object
type WebsiteReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=website.xianyuluo.com,resources=websites,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=website.xianyuluo.com,resources=websites/status,verbs=get;update;patch

func (r *WebsiteReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	_ = context.Background()
	_ = r.Log.WithValues("website", req.NamespacedName)

	// your logic here
	// Fetch the website instance
	instance := &websitev1.Website{}
	fmt.Println("Instance实例内容为：", instance)
	err := r.Client.Get(context.TODO(), req.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	if instance.DeletionTimestamp != nil {
		return reconcile.Result{}, err
	}

	// 一、如果不存在，则创建关联资源
	// 二、如果存在，则判断是否需要更新
	//	1、如果需要更新，则直接更新
	//	2、如果不需要更新，则正常返回
	deploy := &appsv1beta1.Deployment{}
	if err := r.Client.Get(context.TODO(), req.NamespacedName, deploy); err != nil && errors.IsNotFound(err) {
		// 没有找到相关资源，需要创建
		// 1、创建 Deploy
		fmt.Println("创建Deployment")
		deploy := NewDeploy(instance)
		if err := r.Client.Create(context.TODO(), deploy); err != nil {
			return reconcile.Result{}, err
		}

		// 2、创建Service
		fmt.Println("创建Services")
		service := NewService(instance)
		if err := r.Client.Create(context.TODO(), service); err != nil {
			return reconcile.Result{}, err
		}

		// 3、关联 Annotations
		fmt.Println("关联Annotations")
		data, _ := json.Marshal(instance.Spec)
		if instance.Annotations != nil {
			instance.Annotations["spec"] = string(data)
		} else {
			instance.Annotations = map[string]string{"spec": string(data)}
		}

		if err := r.Client.Update(context.TODO(), instance); err != nil {
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, err
	}

	oldspec := websitev1.WebsiteSpec{}
	if err := json.Unmarshal([]byte(instance.Annotations["spec"]), &oldspec); err != nil {
		return reconcile.Result{}, err
	}

	if !reflect.DeepEqual(instance.Spec, oldspec) {
		// 更新关联资源
		fmt.Println("更新关联资源...")
		newDeploy := NewDeploy(instance)
		oldDeploy := &appsv1beta1.Deployment{}

		if err := r.Client.Get(context.TODO(), req.NamespacedName, oldDeploy); err != nil {
			return reconcile.Result{}, err
		}
		oldDeploy.Spec = newDeploy.Spec
		if err := r.Client.Update(context.TODO(), oldDeploy); err != nil {
			return reconcile.Result{}, err
		}

		newService := NewService(instance)
		oldService := &corev1.Service{}
		if err := r.Client.Get(context.TODO(), req.NamespacedName, oldService); err != nil {
			return reconcile.Result{}, err
		}
		oldService.Spec = newService.Spec
		if err := r.Client.Update(context.TODO(), oldService); err != nil {
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, nil
	}
	return ctrl.Result{}, nil
}

func (r *WebsiteReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&websitev1.Website{}).
		Complete(r)
}

// 自己补充的方法

// 1、返回container
func newContainers(app *websitev1.Website) []corev1.Container {
	return []corev1.Container{
		{
			Name:            app.Name,
			Image:           app.Spec.Image,
			Resources:       app.Spec.Resources,
			ImagePullPolicy: corev1.PullIfNotPresent,
		},
	}
}

// 2、创建Deployment
func NewDeploy(app *websitev1.Website) *appsv1beta1.Deployment {
	labels := map[string]string{"app": app.Name}
	selector := &metav1.LabelSelector{MatchLabels: labels}
	return &appsv1beta1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},

		ObjectMeta: metav1.ObjectMeta{
			Name:      app.Name,
			Namespace: app.Namespace,

			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(app, schema.GroupVersionKind{
					Group:   metav1.SchemeGroupVersion.Group,
					Version: metav1.SchemeGroupVersion.Version,
					Kind:    "Website",
				}),
			},
		},

		Spec: appsv1beta1.DeploymentSpec{
			Replicas: app.Spec.Size,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: newContainers(app),
				},
			},
			Selector: selector,
		},
	}
}

// 3、创建Service
func NewService(app *websitev1.Website) *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},

		ObjectMeta: metav1.ObjectMeta{
			Name:      app.Name,
			Namespace: app.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(app, schema.GroupVersionKind{
					Group:   metav1.SchemeGroupVersion.Group,
					Version: metav1.SchemeGroupVersion.Version,
					Kind:    "Website",
				}),
			},
		},

		Spec: corev1.ServiceSpec{
			Type:  corev1.ServiceTypeLoadBalancer,
			Ports: app.Spec.Port,
			Selector: map[string]string{
				"app": app.Name,
			},
		},
	}
}
