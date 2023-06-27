package controllers_test

import (
	"context"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"os"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	selfnoderemediationv1alpha1 "github.com/medik8s/self-node-remediation/api/v1alpha1"
)

var _ = Describe("snrc controller Test", func() {
	dsName := "self-node-remediation-ds"

	Context("DS installation", func() {

		dummySelfNodeRemediationImage := "self-node-remediation-image"
		var config *selfnoderemediationv1alpha1.SelfNodeRemediationConfig
		var ds *appsv1.DaemonSet

		JustBeforeEach(func() {
			Expect(k8sClient.Create(context.Background(), config)).To(Succeed())
			DeferCleanup(func() {
				Expect(k8sClient.Delete(context.Background(), config)).To(Succeed())
				k8sClient.Delete(context.Background(), ds)
			})

		})
		BeforeEach(func() {
			ds = &appsv1.DaemonSet{}
			config = &selfnoderemediationv1alpha1.SelfNodeRemediationConfig{}

			_ = os.Setenv("SELF_NODE_REMEDIATION_IMAGE", dummySelfNodeRemediationImage)
			config.Kind = "SelfNodeRemediationConfig"
			config.APIVersion = "self-node-remediation.medik8s.io/v1alpha1"
			config.Spec.WatchdogFilePath = "/dev/foo"
			config.Spec.SafeTimeToAssumeNodeRebootedSeconds = 123
			config.Name = selfnoderemediationv1alpha1.ConfigCRName
			config.Namespace = namespace

		})

		It("Config CR should be created", func() {
			Expect(k8sClient).To(Not(BeNil()))

			createdConfig := &selfnoderemediationv1alpha1.SelfNodeRemediationConfig{}
			configKey := client.ObjectKeyFromObject(config)

			Eventually(func() error {
				return k8sClient.Get(context.Background(), configKey, createdConfig)
			}, 5*time.Second, 250*time.Millisecond).Should(BeNil())

			Expect(createdConfig.Spec.WatchdogFilePath).To(Equal(config.Spec.WatchdogFilePath))
			Expect(createdConfig.Spec.SafeTimeToAssumeNodeRebootedSeconds).To(Equal(config.Spec.SafeTimeToAssumeNodeRebootedSeconds))
		})

		It("Cert Secret should be created", func() {
			Eventually(func() error {
				_, _, _, err := certReader.GetCerts()
				return err
			}, 15*time.Second, 250*time.Millisecond).ShouldNot(HaveOccurred())
		})

		It("Daemonset should be created", func() {
			key := types.NamespacedName{
				Namespace: namespace,
				Name:      dsName,
			}
			Eventually(func() error {
				return k8sClient.Get(context.Background(), key, ds)
			}, 10*time.Second, 250*time.Millisecond).Should(BeNil())

			dsContainers := ds.Spec.Template.Spec.Containers
			Expect(len(dsContainers)).To(BeNumerically("==", 1))
			container := dsContainers[0]
			Expect(container.Image).To(Equal(dummySelfNodeRemediationImage))
			envVars := getEnvVarMap(container.Env)
			Expect(envVars["WATCHDOG_PATH"].Value).To(Equal(config.Spec.WatchdogFilePath))
			Expect(envVars["TIME_TO_ASSUME_NODE_REBOOTED"].Value).To(Equal("123"))

			Expect(len(ds.OwnerReferences)).To(Equal(1))
			Expect(ds.OwnerReferences[0].Name).To(Equal(config.Name))
			Expect(ds.OwnerReferences[0].Kind).To(Equal("SelfNodeRemediationConfig"))
		})
		When("Configuration has customized tolerations", func() {
			var expectedToleration corev1.Toleration
			BeforeEach(func() {
				expectedToleration = corev1.Toleration{Key: "dummyTolerationKey", Operator: corev1.TolerationOpEqual, Effect: corev1.TaintEffectNoExecute}
				config.Spec.CustomDsTolerations = []corev1.Toleration{expectedToleration}
			})
			It("Daemonset should have customized tolerations", func() {
				key := types.NamespacedName{
					Namespace: namespace,
					Name:      dsName,
				}
				Eventually(func() error {
					return k8sClient.Get(context.Background(), key, ds)
				}, 10*time.Second, 250*time.Millisecond).Should(BeNil())

				//verify toleration is added to ds
				verifyExpectedToleration(ds, &expectedToleration)

				//update configuration
				config.Spec.PeerUpdateInterval = &metav1.Duration{Duration: time.Second * 10}
				Expect(k8sClient.Update(context.Background(), config)).To(Succeed())
				//give the ds time to update
				time.Sleep(time.Second)
				//fetch updated ds
				Expect(k8sClient.Get(context.Background(), key, ds)).To(Succeed())
				//verify ds has new configuration
				envVars := getEnvVarMap(ds.Spec.Template.Spec.Containers[0].Env)
				Expect(envVars["PEER_UPDATE_INTERVAL"].Value).To(Equal(strconv.Itoa(int(time.Second * 10))))
				//verify toleration remains on ds
				verifyExpectedToleration(ds, &expectedToleration)

			})
		})
	})

	Context("SNRC defaults", func() {
		config := &selfnoderemediationv1alpha1.SelfNodeRemediationConfig{}
		config.Kind = "SelfNodeRemediationConfig"
		config.APIVersion = "self-node-remediation.medik8s.io/v1alpha1"
		config.Name = "config-defaults"
		config.Namespace = namespace

		It("Config CR should be created with default values", func() {
			Expect(k8sClient).To(Not(BeNil()))
			Expect(k8sClient.Create(context.Background(), config)).To(Succeed())

			createdConfig := &selfnoderemediationv1alpha1.SelfNodeRemediationConfig{}
			configKey := client.ObjectKeyFromObject(config)

			Eventually(func() error {
				return k8sClient.Get(context.Background(), configKey, createdConfig)
			}, 5*time.Second, 250*time.Millisecond).Should(BeNil())

			Expect(createdConfig.Spec.WatchdogFilePath).To(Equal("/dev/watchdog"))
			Expect(createdConfig.Spec.SafeTimeToAssumeNodeRebootedSeconds).To(Equal(180))
			Expect(createdConfig.Spec.MaxApiErrorThreshold).To(Equal(3))

			Expect(createdConfig.Spec.PeerApiServerTimeout.Seconds()).To(BeEquivalentTo(5))
			Expect(createdConfig.Spec.PeerRequestTimeout.Seconds()).To(BeEquivalentTo(5))
			Expect(createdConfig.Spec.PeerDialTimeout.Seconds()).To(BeEquivalentTo(5))
			Expect(createdConfig.Spec.ApiServerTimeout.Seconds()).To(BeEquivalentTo(5))
			Expect(createdConfig.Spec.ApiCheckInterval.Seconds()).To(BeEquivalentTo(15))
			Expect(createdConfig.Spec.PeerUpdateInterval.Seconds()).To(BeEquivalentTo(15 * 60))
		})
	})

	Context("Wrong self node remediation config CR", func() {
		var dsResourceVersion string
		var key types.NamespacedName
		BeforeEach(func() {
			key = types.NamespacedName{
				Namespace: namespace,
				Name:      dsName,
			}
			ds := &appsv1.DaemonSet{}
			By("get DS resource version")
			Expect(k8sClient.Get(context.Background(), key, ds)).To(Succeed())
			dsResourceVersion = ds.ResourceVersion

			By("create a config CR")
			config := &selfnoderemediationv1alpha1.SelfNodeRemediationConfig{}
			config.Kind = "SelfNodeRemediationConfig"
			config.APIVersion = "self-node-remediation.medik8s.io/v1alpha1"
			config.Name = "not-the-expected-name"
			config.Spec.WatchdogFilePath = "foo"
			config.Spec.SafeTimeToAssumeNodeRebootedSeconds = 9999
			config.Namespace = namespace

			Expect(k8sClient.Create(context.Background(), config)).To(Succeed())
		})

		It("Daemonset should not be changed", func() {
			ds := &appsv1.DaemonSet{}
			Consistently(func() string {
				Expect(k8sClient.Get(context.Background(), key, ds)).To(Succeed())
				return ds.ResourceVersion
			}, 20*time.Second, 1*time.Second).Should(Equal(dsResourceVersion))
		})
	})

})

func verifyExpectedToleration(ds *appsv1.DaemonSet, expectedToleration *corev1.Toleration) {
	Expect(ds.Spec.Template.Spec.Tolerations).ToNot(BeNil())
	Expect(len(ds.Spec.Template.Spec.Tolerations)).To(Equal(4))

	var actualToleration corev1.Toleration
	for _, t := range ds.Spec.Template.Spec.Tolerations {
		if t.Key == expectedToleration.Key {
			actualToleration = t
			break
		}
	}
	//Verify customized toleration found
	Expect(actualToleration).ToNot(BeNil())
	Expect(string(expectedToleration.Effect)).To(Equal(string(actualToleration.Effect)))
	Expect(string(expectedToleration.Operator)).To(Equal(string(actualToleration.Operator)))
}

func getEnvVarMap(vars []corev1.EnvVar) map[string]corev1.EnvVar {
	m := map[string]corev1.EnvVar{}
	for _, envVar := range vars {
		m[envVar.Name] = envVar
	}
	return m
}
