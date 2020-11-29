# 背景
在工作中经常会有使用`k8s`部署站点应用，一般会创建两个编排文件：`deployment.yaml`和`svc.yaml`，但是有`operator`概念之后，其实我们可以自定义一个`operator`来帮忙创建`deployment`和`svc`。
此篇文章的目的就是通过编写一个自定义的`website-operator`来实现此功能。只需要提供一个简单的`yaml`文件，就可以实现需求。
样例`YAML`：
```yaml
apiVersion: website.xianyuluo.com/v1
kind: Website
metadata:
  name: nginx-app
  namespace: website-operator-system
spec:
  size: 3
  image: xianyuluo/nginx:1.12.2.website-operator
  port:
    - port: 80
      targetPort: 80
```

`website-operator`可以根据上面的`yaml`文件自动部署`Deployment`和`SVC`。

# 实现
使用`CoreOS`公司开源的`operator-sdk`框架实现。框架的内容可以[参考官网](https://sdk.operatorframework.io/docs/building-operators/golang/tutorial/)，框架其实比较简单，核心的东西就是`kubernetes`的`golang`客户端，这里就不在赘述了。

## 需要自己动手的代码
### website_types.go
自定义`operator`的**编排文件格式**和**实例状态**
```golang
// WebsiteSpec defines the desired state of Website
type WebsiteSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	Size      *int32                      `json:"size"`
	Image     string                      `json:"image"`
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
	Envs      []corev1.EnvVar             `json:"envs,omitempty"`
	Port      []corev1.ServicePort        `json:"port,omitempty"`
}

...

// WebsiteStatus defines the observed state of Website
type WebsiteStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	v1beta1.DeploymentStatus `json:",inline"`
}
```

### website_controller.go
所有逻辑都在由框架自动生成的`Reconcile`方法中，其他的自己任意补充。
```golang
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

//+kubebuilder:rbac:groups=website.xianyuluo.com,resources=websites,verbs=get;list;watch;create;update;patch;delete
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
```

# 最终效果
### Operator部署
部署成功后，会创建出一个`deployment`和一个`service`，`service`是用于监控应用监控，可以先不管
![在这里插入图片描述](https://img-blog.csdnimg.cn/20201129174311618.png?x-oss-process=image/watermark,type_ZmFuZ3poZW5naGVpdGk,shadow_10,text_aHR0cHM6Ly9ibG9nLmNzZG4ubmV0L0ZyZWVfdGltZV8=,size_16,color_FFFFFF,t_70)
### 创建website
一、根据最开始的提到的`简易yaml`来创建`website`实例
`test-website.yaml`
```yaml
apiVersion: website.xianyuluo.com/v1
kind: Website
metadata:
  name: nginx-app
  namespace: website-operator-system
spec:
  size: 1
  image: xianyuluo/nginx:1.12.2.website-operator
  port:
    - port: 80
      targetPort: 80
```
![在这里插入图片描述](https://img-blog.csdnimg.cn/20201129174649413.png?x-oss-process=image/watermark,type_ZmFuZ3poZW5naGVpdGk,shadow_10,text_aHR0cHM6Ly9ibG9nLmNzZG4ubmV0L0ZyZWVfdGltZV8=,size_16,color_FFFFFF,t_70)

二、观察集群中的`deployment`和`service`
`deployment`和`service`已经由`website-operator`帮我们创建出来了，Nice~（`svc`默认为`LoadBalancer`类型）。访问一下看看！
![在这里插入图片描述](https://img-blog.csdnimg.cn/20201129174720760.png?x-oss-process=image/watermark,type_ZmFuZ3poZW5naGVpdGk,shadow_10,text_aHR0cHM6Ly9ibG9nLmNzZG4ubmV0L0ZyZWVfdGltZV8=,size_16,color_FFFFFF,t_70)

![在这里插入图片描述](https://img-blog.csdnimg.cn/20201129174917741.png?x-oss-process=image/watermark,type_ZmFuZ3poZW5naGVpdGk,shadow_10,text_aHR0cHM6Ly9ibG9nLmNzZG4ubmV0L0ZyZWVfdGltZV8=,size_16,color_FFFFFF,t_70)

站点正常，Good!

# 参考文档
[https://github.com/xianyuLuo/website-operator](https://github.com/xianyuLuo/website-operator)

[https://sdk.operatorframework.io/docs/building-operators/golang/tutorial/](https://sdk.operatorframework.io/docs/building-operators/golang/tutorial/)

[https://www.qikqiak.com/post/k8s-operator-101/](https://www.qikqiak.com/post/k8s-operator-101/)

记录的不是很详细，有些知识自己也还在琢磨当中，后面再补充！