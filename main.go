/*
@Desc : k8s client-go demo
@Time : 2018/7/24 9:35 
@Author : Caimw
*/

package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	appsv1 "k8s.io/api/apps/v1"
	apiv1 "k8s.io/api/core/v1"
	extsv1beta "k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/retry"
	"os"
	"path/filepath"

	// Uncomment the following line to load the gcp plugin (only required to authenticate against GKE clusters).
	// _ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

// 初始化连接并返回client实例集合
func initClientSet() (*kubernetes.Clientset, error) {
	var kubeconfig *string
	if home := homeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	flag.Parse()

	// 如果应用跑在集群内部，则用这种方式初始化config
	//config, err := rest.InClusterConfig()

	// 使用.kube/config构建clientconfig(应用跑在集群外部)
	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		return nil, err
	}

	// 创建各个group/version的client
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return clientset, nil
}

func podTest(clientset *kubernetes.Clientset) {
	// 获取所有pod
	pods, err := clientset.CoreV1().Pods("").List(metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}

	for _, pod := range pods.Items {
		fmt.Printf("namespace：%s, name: %s, status: %v, startTime: %v\n",
			pod.Namespace, pod.Name, pod.Status.Phase, pod.Status.StartTime)
	}
	fmt.Printf("Total: %d pods\n", len(pods.Items))

	// 获取指定pod
	namespace := "kube-system"
	pod := "kube-apiserver-docker-for-desktop"
	_, err = clientset.CoreV1().Pods(namespace).Get(pod, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		fmt.Printf("Pod %q in namespace %q not found\n", pod, namespace)
	} else if statusError, isStatus := err.(*errors.StatusError); isStatus {
		fmt.Printf("Error getting pod %q in namespace %q: %v\n",
			pod, namespace, statusError.ErrStatus.Message)
	} else if err != nil {
		panic(err.Error())
	} else {
		fmt.Printf("Found pod %q in namespace %q\n", pod, namespace)
	}

	// TODO 创建、更改、删除pod（这些操作推荐使用deployment管理）
}

func deployTest(clientset *kubernetes.Clientset) {
	// 通过namespace获取到相应client
	deploymentsClient := clientset.AppsV1().Deployments(apiv1.NamespaceDefault)
	// ExtensionsV1beta1的client也是实现了deployment的CRUD方法，如果用这种方式，注意更改Deployment结构体的路径
	//deploymentsClient := clientset.ExtensionsV1beta1().Deployments(apiv1.NamespaceDefault)

	// 结构体构建（也可以读取json文件进行unmarshall）
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: "demo-deployment",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(2),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "demo",
				},
			},
			Template: apiv1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "demo",
					},
				},
				Spec: apiv1.PodSpec{
					Containers: []apiv1.Container{
						{
							Name:  "web",
							Image: "nginx:1.12",
							Ports: []apiv1.ContainerPort{
								{
									Name:          "http",
									Protocol:      apiv1.ProtocolTCP,
									ContainerPort: 80,
								},
							},
						},
					},
				},
			},
		},
	}

	// 创建Deployment
	fmt.Println("Creating deployment...")
	result, err := deploymentsClient.Create(deployment)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Created deployment %q.\n", result.GetObjectMeta().GetName())

	// 更新Deployment
	prompt()
	fmt.Println("Updating deployment...")

	// 更新有两种方式
	// 1. 修改deployment变量后，然后调用Update(deployment)，这种方式类似于kubectl replace，
	//    但是这种方式会导致Create()和Update()方法中间的更改覆盖或者丢失
	//
	// 2. 先Get()得到返回的result变量，更改后再一直调用Update()直到不会获取到conflict error，这样
	//    就可以保留其他客户端在你进行Create()和Update()中进行的修改。
	//
	// 官方推荐使用第二种方式
	// 下面按照第二种方式进行实现
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// 获取最新Deployment
		result, getErr := deploymentsClient.Get("demo-deployment", metav1.GetOptions{})
		if getErr != nil {
			panic(fmt.Errorf("Failed to get latest version of Deployment: %v", getErr))
		}

		// 修改replica和image，进行更新
		result.Spec.Replicas = int32Ptr(1)
		result.Spec.Template.Spec.Containers[0].Image = "nginx:1.13"
		_, updateErr := deploymentsClient.Update(result)
		return updateErr
	})

	if retryErr != nil {
		panic(fmt.Errorf("Update failed: %v", retryErr))
	}
	fmt.Println("Updated deployment...")

	// 获取所有Deployments
	prompt()
	fmt.Printf("Listing deployments in namespace %q:\n", apiv1.NamespaceDefault)
	list, err := deploymentsClient.List(metav1.ListOptions{})
	if err != nil {
		panic(err)
	}
	for _, d := range list.Items {
		fmt.Printf(" * %s (%d replicas)\n", d.Name, *d.Spec.Replicas)
	}

	// 删除Deployment
	prompt()
	fmt.Println("Deleting deployment...")
	deletePolicy := metav1.DeletePropagationForeground // 垃圾(相关依赖)回收机制设置为前端删除，这样删除出错会有错误返回
	if err := deploymentsClient.Delete("demo-deployment", &metav1.DeleteOptions{
		PropagationPolicy: &deletePolicy,
	}); err != nil {
		panic(err)
	}
	fmt.Println("Deleted deployment.")
}

func svcTest(clientset *kubernetes.Clientset) {
	deploysClient := clientset.ExtensionsV1beta1().Deployments(apiv1.NamespaceDefault)

	// 从json文件读取deploy信息
	data, err := ioutil.ReadFile("demo-deploy.json")
	if err != nil {
		panic(err)
	}
	//fmt.Println(string(data))

	// 反序列化为结构体
	var deployment extsv1beta.Deployment
	err = json.Unmarshal(data, &deployment)
	if err != nil {
		panic(err)
	}

	// 创建Deployment
	fmt.Println("Creating deployment...")
	result, err := deploysClient.Create(&deployment)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Created deployment %q.\n", result.GetObjectMeta().GetName())

	prompt()
	svcsClient := clientset.CoreV1().Services(apiv1.NamespaceDefault)

	// 从json文件读取svc信息
	data, err = ioutil.ReadFile("demo-service.json")
	if err != nil {
		panic(err)
	}
	//fmt.Println(string(data))

	var service apiv1.Service
	err = json.Unmarshal(data, &service)
	if err != nil {
		panic(err)
	}

	// 创建Service
	fmt.Println("Creating Service...")
	resultSvc, err := svcsClient.Create(&service)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Created Service %q.\n", resultSvc.GetObjectMeta().GetName())

	prompt()
	// 更新Service
	fmt.Println("Updating Service...")
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		result, err := svcsClient.Get(service.Name, metav1.GetOptions{})
		if err != nil {
			panic(fmt.Errorf("Failed to get latest version of Service: %v", err))
		}

		result.Spec.Ports[0].NodePort = 30000
		_, err = svcsClient.Update(result)
		return err
	})
	if retryErr != nil {
		panic(fmt.Errorf("Update failed: %v", retryErr))
	}
	fmt.Printf("Updated Service %q\n", service.Name)

	prompt()
	// 删除Service
	fmt.Println("Deleting Service...")
	deletePolicy := metav1.DeletePropagationForeground // 垃圾(相关依赖)回收机制设置为前端删除，这样删除出错会有错误返回
	err = svcsClient.Delete(service.Name, &metav1.DeleteOptions{PropagationPolicy: &deletePolicy})
	if err != nil {
		panic(err)
	}
	fmt.Printf("Deleted Service %q.\n", service.Name)

	prompt()
	// 删除Deployment
	fmt.Println("Deleting deployment...")
	err = deploysClient.Delete(deployment.Name, &metav1.DeleteOptions{PropagationPolicy: &deletePolicy})
	if err != nil {
		panic(err)
	}
	fmt.Printf("Deleted deployment %q.\n", deployment.Name)
}

// 监听资源变化
func startWatch(clientset *kubernetes.Clientset) {
	deployClient := clientset.AppsV1().Deployments(metav1.NamespaceDefault)
	svcClient := clientset.CoreV1().Services(metav1.NamespaceDefault)

	go func() {
		w, _ := deployClient.Watch(metav1.ListOptions{})
		for {
			select {
			case e, _ := <-w.ResultChan():
				fmt.Println(e.Type, e.Object)
			}
		}
	}()

	go func() {
		w, _ := svcClient.Watch(metav1.ListOptions{})
		for {
			select {
			case e, _ := <-w.ResultChan():
				fmt.Println(e.Type, e.Object)
			}
		}
	}()
}

// 返回int32指针
func int32Ptr(i int32) *int32 {
	return &i
}

func prompt() {
	fmt.Printf("-> 按回车继续...")
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		break
	}
	if err := scanner.Err(); err != nil {
		panic(err)
	}
	fmt.Println()
}

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return os.Getenv("USERPROFILE") // windows
}

func main() {
	clientset, err := initClientSet()
	if err != nil {
		panic(err)
	}

	//startWatch(clientset)
	podTest(clientset)
	//deployTest(clientset)
	//svcTest(clientset)
}
