//go:build e2e
// +build e2e

package kafka_test

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/joho/godotenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes"

	. "github.com/kedacore/keda/v2/tests/helper"
)

// Load environment variables from .env file
var _ = godotenv.Load("../../.env")

const (
	testName = "kafka-test"
)

var (
	testNamespace                 = fmt.Sprintf("%s-ns", testName)
	deploymentName                = fmt.Sprintf("%s-deployment", testName)
	kafkaName                     = fmt.Sprintf("%s-kafka", testName)
	kafkaClientName               = fmt.Sprintf("%s-client", testName)
	scaledObjectName              = fmt.Sprintf("%s-so", testName)
	bootstrapServer               = fmt.Sprintf("%s-kafka-bootstrap.%s:9092", kafkaName, testNamespace)
	topic1                        = "kafka-topic"
	topic2                        = "kafka-topic2"
	zeroInvalidOffsetTopic        = "kafka-topic-zero-invalid-offset"
	oneInvalidOffsetTopic         = "kafka-topic-one-invalid-offset"
	invalidOffsetGroup            = "invalidOffset"
	persistentLagTopic            = "kafka-topic-persistent-lag"
	persistentLagGroup            = "persistentLag"
	persistentLagDeploymentGroup  = "persistentLagDeploymentGroup"
	limitToPartitionsWithLagTopic = "limit-to-partitions-with-lag"
	limitToPartitionsWithLagGroup = "limitToPartitionsWithLag"
	topicPartitions               = 3
)

type templateData struct {
	TestNamespace            string
	DeploymentName           string
	ScaledObjectName         string
	KafkaName                string
	KafkaTopicName           string
	KafkaTopicPartitions     int
	KafkaClientName          string
	TopicName                string
	Topic1Name               string
	Topic2Name               string
	BootstrapServer          string
	ResetPolicy              string
	Params                   string
	Commit                   string
	ScaleToZeroOnInvalid     string
	ExcludePersistentLag     string
	LimitToPartitionsWithLag string
}

const (
	singleDeploymentTemplate = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{.DeploymentName}}
  namespace: {{.TestNamespace}}
  labels:
    app: {{.DeploymentName}}
spec:
  replicas: 0
  selector:
    matchLabels:
      app: kafka-consumer
  template:
    metadata:
      labels:
        app: kafka-consumer
    spec:
      containers:
      # only recent version of kafka-console-consumer support flag "include"
      # old version's equiv flag will violate language-matters commit hook
      # work around -> create two consumer container joining the same group
      - name: kafka-consumer
        image: confluentinc/cp-kafka:5.2.1
        command:
          - sh
          - -c
          - "kafka-console-consumer --bootstrap-server {{.BootstrapServer}} {{.Params}} --consumer-property enable.auto.commit={{.Commit}}"
`

	multiDeploymentTemplate = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{.DeploymentName}}
  namespace: {{.TestNamespace}}
  labels:
    app: {{.DeploymentName}}
spec:
  replicas: 0
  selector:
    matchLabels:
      app: kafka-consumer
  template:
    metadata:
      labels:
        app: kafka-consumer
    spec:
      containers:
      # only recent version of kafka-console-consumer support flag "include"
      # old version's equiv flag will violate language-matters commit hook
      # work around -> create two consumer container joining the same group
      - name: kafka-consumer
        image: confluentinc/cp-kafka:5.2.1
        command:
          - sh
          - -c
          - "kafka-console-consumer --bootstrap-server {{.BootstrapServer}} --topic '{{.Topic1Name}}'  --group multiTopic --from-beginning --consumer-property enable.auto.commit=false"
      - name: kafka-consumer-2
        image: confluentinc/cp-kafka:5.2.1
        command:
          - sh
          - -c
          - "kafka-console-consumer --bootstrap-server {{.BootstrapServer}} --topic '{{.Topic2Name}}' --group multiTopic --from-beginning --consumer-property enable.auto.commit=false"
`

	singleScaledObjectTemplate = `
apiVersion: keda.sh/v1alpha1
kind: ScaledObject
metadata:
  name: {{.ScaledObjectName}}
  namespace: {{.TestNamespace}}
  labels:
    app: {{.DeploymentName}}
spec:
  pollingInterval: 5
  cooldownPeriod: 0
  scaleTargetRef:
    name: {{.DeploymentName}}
  advanced:
    horizontalPodAutoscalerConfig:
      behavior:
        scaleUp:
          stabilizationWindowSeconds: 0
          policies:
          - type: Percent
            value: 100
            periodSeconds: 15
        scaleDown:
          stabilizationWindowSeconds: 0
          policies:
          - type: Percent
            value: 100
            periodSeconds: 15
  triggers:
  - type: kafka
    metadata:
      topic: {{.TopicName}}
      bootstrapServers: {{.BootstrapServer}}
      consumerGroup: {{.ResetPolicy}}
      lagThreshold: '1'
      activationLagThreshold: '1'
      offsetResetPolicy: {{.ResetPolicy}}`

	multiScaledObjectTemplate = `
apiVersion: keda.sh/v1alpha1
kind: ScaledObject
metadata:
  name: {{.ScaledObjectName}}
  namespace: {{.TestNamespace}}
  labels:
    app: {{.DeploymentName}}
spec:
  pollingInterval: 5
  cooldownPeriod: 0
  scaleTargetRef:
    name: {{.DeploymentName}}
  advanced:
    horizontalPodAutoscalerConfig:
      behavior:
        scaleUp:
          stabilizationWindowSeconds: 0
          policies:
          - type: Percent
            value: 100
            periodSeconds: 15
        scaleDown:
          stabilizationWindowSeconds: 0
          policies:
          - type: Percent
            value: 100
            periodSeconds: 15
  triggers:
  - type: kafka
    metadata:
      bootstrapServers: {{.BootstrapServer}}
      consumerGroup: multiTopic
      lagThreshold: '1'
      offsetResetPolicy: 'latest'`

	invalidOffsetWithLatestOffsetResetPolicyScaledObjectTemplate = `
apiVersion: keda.sh/v1alpha1
kind: ScaledObject
metadata:
  name: {{.ScaledObjectName}}
  namespace: {{.TestNamespace}}
  labels:
    app: {{.DeploymentName}}
spec:
  pollingInterval: 5
  cooldownPeriod: 0
  scaleTargetRef:
    name: {{.DeploymentName}}
  advanced:
    horizontalPodAutoscalerConfig:
      behavior:
        scaleUp:
          stabilizationWindowSeconds: 0
          policies:
          - type: Percent
            value: 100
            periodSeconds: 15
        scaleDown:
          stabilizationWindowSeconds: 0
          policies:
          - type: Percent
            value: 100
            periodSeconds: 15
  triggers:
  - type: kafka
    metadata:
      topic: {{.TopicName}}
      bootstrapServers: {{.BootstrapServer}}
      consumerGroup: {{.ResetPolicy}}
      lagThreshold: '1'
      scaleToZeroOnInvalidOffset: '{{.ScaleToZeroOnInvalid}}'
      offsetResetPolicy: 'latest'`

	invalidOffsetWithEarliestOffsetResetPolicyScaledObjectTemplate = `
apiVersion: keda.sh/v1alpha1
kind: ScaledObject
metadata:
  name: {{.ScaledObjectName}}
  namespace: {{.TestNamespace}}
  labels:
    app: {{.DeploymentName}}
spec:
  pollingInterval: 5
  cooldownPeriod: 0
  scaleTargetRef:
    name: {{.DeploymentName}}
  advanced:
    horizontalPodAutoscalerConfig:
      behavior:
        scaleUp:
          stabilizationWindowSeconds: 0
          policies:
          - type: Percent
            value: 100
            periodSeconds: 15
        scaleDown:
          stabilizationWindowSeconds: 0
          policies:
          - type: Percent
            value: 100
            periodSeconds: 15
  triggers:
  - type: kafka
    metadata:
      topic: {{.TopicName}}
      bootstrapServers: {{.BootstrapServer}}
      consumerGroup: {{.ResetPolicy}}
      lagThreshold: '1'
      scaleToZeroOnInvalidOffset: '{{.ScaleToZeroOnInvalid}}'
      offsetResetPolicy: 'earliest'`

	persistentLagScaledObjectTemplate = `
apiVersion: keda.sh/v1alpha1
kind: ScaledObject
metadata:
  name: {{.ScaledObjectName}}
  namespace: {{.TestNamespace}}
  labels:
    app: {{.DeploymentName}}
spec:
  pollingInterval: 5
  cooldownPeriod: 0
  scaleTargetRef:
    name: {{.DeploymentName}}
  advanced:
    horizontalPodAutoscalerConfig:
      behavior:
        scaleUp:
          stabilizationWindowSeconds: 0
          policies:
          - type: Percent
            value: 100
            periodSeconds: 15
        scaleDown:
          stabilizationWindowSeconds: 0
          policies:
          - type: Percent
            value: 100
            periodSeconds: 15
  triggers:
  - type: kafka
    metadata:
      topic: {{.TopicName}}
      bootstrapServers: {{.BootstrapServer}}
      consumerGroup: {{.ResetPolicy}}
      lagThreshold: '1'
      excludePersistentLag: '{{.ExcludePersistentLag}}'
      offsetResetPolicy: 'latest'`

	limitToPartionsWithLagScaledObjectTemplate = `
apiVersion: keda.sh/v1alpha1
kind: ScaledObject
metadata:
  name: {{.ScaledObjectName}}
  namespace: {{.TestNamespace}}
  labels:
    app: {{.DeploymentName}}
spec:
  pollingInterval: 5
  cooldownPeriod: 0
  scaleTargetRef:
    name: {{.DeploymentName}}
  advanced:
    horizontalPodAutoscalerConfig:
      behavior:
        scaleUp:
          stabilizationWindowSeconds: 0
          policies:
          - type: Percent
            value: 100
            periodSeconds: 15
        scaleDown:
          stabilizationWindowSeconds: 0
          policies:
          - type: Percent
            value: 100
            periodSeconds: 15
  triggers:
  - type: kafka
    metadata:
      topic: {{.TopicName}}
      bootstrapServers: {{.BootstrapServer}}
      consumerGroup:  {{.ResetPolicy}}
      offsetResetPolicy: 'earliest'
      lagThreshold: '1'
      activationLagThreshold: '1'
      limitToPartitionsWithLag: '{{.LimitToPartitionsWithLag}}'`

	kafkaClusterTemplate = `apiVersion: kafka.strimzi.io/v1beta2
kind: Kafka
metadata:
  name: {{.KafkaName}}
  namespace: {{.TestNamespace}}
spec:
  kafka:
    version: "3.4.0"
    replicas: 1
    listeners:
      - name: plain
        port: 9092
        type: internal
        tls: false
      - name: tls
        port: 9093
        type: internal
        tls: true
    config:
      offsets.topic.replication.factor: 1
      transaction.state.log.replication.factor: 1
      transaction.state.log.min.isr: 1
      log.message.format.version: "2.5"
    storage:
      type: ephemeral
  zookeeper:
    replicas: 1
    storage:
      type: ephemeral
  entityOperator:
    topicOperator: {}
    userOperator: {}
`

	kafkaTopicTemplate = `apiVersion: kafka.strimzi.io/v1beta2
kind: KafkaTopic
metadata:
  name: {{.KafkaTopicName}}
  namespace: {{.TestNamespace}}
  labels:
    strimzi.io/cluster: {{.KafkaName}}
  namespace: {{.TestNamespace}}
spec:
  partitions: {{.KafkaTopicPartitions}}
  replicas: 1
  config:
    retention.ms: 604800000
    segment.bytes: 1073741824
`
	kafkaClientTemplate = `
apiVersion: v1
kind: Pod
metadata:
  name: {{.KafkaClientName}}
  namespace: {{.TestNamespace}}
spec:
  containers:
  - name: {{.KafkaClientName}}
    image: confluentinc/cp-kafka:5.2.1
    command:
      - sh
      - -c
      - "exec tail -f /dev/null"
    securityContext:
      allowPrivilegeEscalation: false
      runAsNonRoot: true
      capabilities:
        drop:
          - ALL
      seccompProfile:
        type: RuntimeDefault`
)

func TestScaler(t *testing.T) {
	// setup
	t.Log("--- setting up ---")
	// Create kubernetes resources
	kc := GetKubernetesClient(t)
	data, templates := getTemplateData()
	CreateKubernetesResources(t, kc, testNamespace, data, templates)
	t.Cleanup(func() {
		DeleteKubernetesResources(t, testNamespace, data, templates)
	})
	addCluster(t, data)
	addTopic(t, data, topic1, topicPartitions)
	addTopic(t, data, topic2, topicPartitions)
	addTopic(t, data, zeroInvalidOffsetTopic, 1)
	addTopic(t, data, oneInvalidOffsetTopic, 1)
	addTopic(t, data, persistentLagTopic, topicPartitions)
	addTopic(t, data, limitToPartitionsWithLagTopic, topicPartitions)

	// test scaling
	testEarliestPolicy(t, kc, data)
	testLatestPolicy(t, kc, data)
	testMultiTopic(t, kc, data)
	testZeroOnInvalidOffsetWithLatestOffsetResetPolicy(t, kc, data)
	testZeroOnInvalidOffsetWithEarliestOffsetResetPolicy(t, kc, data)
	testOneOnInvalidOffsetWithLatestOffsetResetPolicy(t, kc, data)
	testOneOnInvalidOffsetWithEarliestOffsetResetPolicy(t, kc, data)
	testPersistentLag(t, kc, data)
	testScalingOnlyPartitionsWithLag(t, kc, data)
}

func testEarliestPolicy(t *testing.T, kc *kubernetes.Clientset, data templateData) {
	t.Log("--- testing earliest policy: scale out ---")
	data.Params = fmt.Sprintf("--topic %s --group earliest --from-beginning", topic1)
	data.Commit = StringFalse
	data.TopicName = topic1
	data.ResetPolicy = "earliest"
	KubectlApplyWithTemplate(t, data, "singleDeploymentTemplate", singleDeploymentTemplate)
	defer KubectlDeleteWithTemplate(t, data, "singleDeploymentTemplate", singleDeploymentTemplate)
	KubectlApplyWithTemplate(t, data, "singleScaledObjectTemplate", singleScaledObjectTemplate)
	defer KubectlDeleteWithTemplate(t, data, "singleScaledObjectTemplate", singleScaledObjectTemplate)

	// Shouldn't scale pods applying earliest policy
	AssertReplicaCountNotChangeDuringTimePeriod(t, kc, deploymentName, testNamespace, 0, 30)

	// Shouldn't scale pods with only 1 message due to activation value
	publishMessage(t, topic1)
	AssertReplicaCountNotChangeDuringTimePeriod(t, kc, deploymentName, testNamespace, 0, 30)

	// Scale application with kafka messages
	publishMessage(t, topic1)
	assert.True(t, WaitForDeploymentReplicaReadyCount(t, kc, deploymentName, testNamespace, 2, 60, 2),
		"replica count should be %d after 2 minute", 2)

	// Scale application beyond partition max.
	messages := 5
	for i := 0; i < messages; i++ {
		publishMessage(t, topic1)
	}

	assert.True(t, WaitForDeploymentReplicaReadyCount(t, kc, deploymentName, testNamespace, topicPartitions, 60, 2),
		"replica count should be %d after 2 minute", messages)
}

func testLatestPolicy(t *testing.T, kc *kubernetes.Clientset, data templateData) {
	t.Log("--- testing latest policy: scale out ---")
	commitPartition(t, topic1, "latest")
	data.Params = fmt.Sprintf("--topic %s --group latest", topic1)
	data.Commit = StringFalse
	data.TopicName = topic1
	data.ResetPolicy = "latest"
	KubectlApplyWithTemplate(t, data, "singleDeploymentTemplate", singleDeploymentTemplate)
	defer KubectlDeleteWithTemplate(t, data, "singleDeploymentTemplate", singleDeploymentTemplate)
	KubectlApplyWithTemplate(t, data, "singleScaledObjectTemplate", singleScaledObjectTemplate)
	defer KubectlDeleteWithTemplate(t, data, "singleScaledObjectTemplate", singleScaledObjectTemplate)

	// Shouldn't scale pods
	AssertReplicaCountNotChangeDuringTimePeriod(t, kc, deploymentName, testNamespace, 0, 30)

	// Shouldn't scale pods with only 1 message due to activation value
	publishMessage(t, topic1)
	AssertReplicaCountNotChangeDuringTimePeriod(t, kc, deploymentName, testNamespace, 0, 30)

	// Scale application with kafka messages
	publishMessage(t, topic1)
	assert.True(t, WaitForDeploymentReplicaReadyCount(t, kc, deploymentName, testNamespace, 2, 60, 2),
		"replica count should be %d after 2 minute", 2)

	// Scale application beyond partition max.
	messages := 5
	for i := 0; i < messages; i++ {
		publishMessage(t, topic1)
	}

	assert.True(t, WaitForDeploymentReplicaReadyCount(t, kc, deploymentName, testNamespace, topicPartitions, 60, 2),
		"replica count should be %d after 2 minute", messages)
}

func testMultiTopic(t *testing.T, kc *kubernetes.Clientset, data templateData) {
	t.Log("--- testing multi topic: scale out ---")
	commitPartition(t, topic1, "multiTopic")
	commitPartition(t, topic2, "multiTopic")
	KubectlApplyWithTemplate(t, data, "multiDeploymentTemplate", multiDeploymentTemplate)
	defer KubectlDeleteWithTemplate(t, data, "multiDeploymentTemplate", multiDeploymentTemplate)
	KubectlApplyWithTemplate(t, data, "multiScaledObjectTemplate", multiScaledObjectTemplate)
	defer KubectlDeleteWithTemplate(t, data, "multiScaledObjectTemplate", multiScaledObjectTemplate)

	// Shouldn't scale pods
	AssertReplicaCountNotChangeDuringTimePeriod(t, kc, deploymentName, testNamespace, 0, 30)

	// Scale application with kafka messages in topic 1
	publishMessage(t, topic1)
	assert.True(t, WaitForDeploymentReplicaReadyCount(t, kc, deploymentName, testNamespace, 1, 60, 2),
		"replica count should be %d after 2 minute", 1)

	// Scale application with kafka messages in topic 2
	// // produce one more msg to the different topic within the same group
	// // will turn total consumer group lag to 2.
	// // with lagThreshold as 1 -> making hpa AverageValue to 1
	// // this should turn nb of replicas to 2
	// // as desiredReplicaCount = totalLag / avgThreshold
	publishMessage(t, topic2)
	assert.True(t, WaitForDeploymentReplicaReadyCount(t, kc, deploymentName, testNamespace, 2, 60, 2),
		"replica count should be %d after 2 minute", 2)
}

func testZeroOnInvalidOffsetWithLatestOffsetResetPolicy(t *testing.T, kc *kubernetes.Clientset, data templateData) {
	t.Log("--- testing zeroInvalidOffsetTopicWithLatestOffsetResetPolicy: scale out ---")
	data.Params = fmt.Sprintf("--topic %s --group %s", zeroInvalidOffsetTopic, invalidOffsetGroup)
	data.Commit = StringTrue
	data.TopicName = zeroInvalidOffsetTopic
	data.ResetPolicy = invalidOffsetGroup
	data.ScaleToZeroOnInvalid = StringTrue
	KubectlApplyWithTemplate(t, data, "singleDeploymentTemplate", singleDeploymentTemplate)
	defer KubectlDeleteWithTemplate(t, data, "singleDeploymentTemplate", singleDeploymentTemplate)
	KubectlApplyWithTemplate(t, data, "invalidOffsetWithLatestOffsetResetPolicyScaledObjectTemplate", invalidOffsetWithLatestOffsetResetPolicyScaledObjectTemplate)
	defer KubectlDeleteWithTemplate(t, data, "invalidOffsetWithLatestOffsetResetPolicyScaledObjectTemplate", invalidOffsetWithLatestOffsetResetPolicyScaledObjectTemplate)

	// Shouldn't scale pods
	AssertReplicaCountNotChangeDuringTimePeriod(t, kc, deploymentName, testNamespace, 0, 30)
}

func testZeroOnInvalidOffsetWithEarliestOffsetResetPolicy(t *testing.T, kc *kubernetes.Clientset, data templateData) {
	t.Log("--- testing zeroInvalidOffsetTopicWithEarliestOffsetResetPolicy: scale out ---")
	data.Params = fmt.Sprintf("--topic %s --group %s", zeroInvalidOffsetTopic, invalidOffsetGroup)
	data.Commit = StringTrue
	data.TopicName = zeroInvalidOffsetTopic
	data.ResetPolicy = invalidOffsetGroup
	data.ScaleToZeroOnInvalid = StringTrue
	KubectlApplyWithTemplate(t, data, "singleDeploymentTemplate", singleDeploymentTemplate)
	defer KubectlDeleteWithTemplate(t, data, "singleDeploymentTemplate", singleDeploymentTemplate)
	KubectlApplyWithTemplate(t, data, "invalidOffsetWithEarliestOffsetResetPolicyScaledObjectTemplate", invalidOffsetWithEarliestOffsetResetPolicyScaledObjectTemplate)
	defer KubectlDeleteWithTemplate(t, data, "invalidOffsetWithEarliestOffsetResetPolicyScaledObjectTemplate", invalidOffsetWithEarliestOffsetResetPolicyScaledObjectTemplate)

	// Shouldn't scale pods
	AssertReplicaCountNotChangeDuringTimePeriod(t, kc, deploymentName, testNamespace, 0, 30)
}

func testOneOnInvalidOffsetWithLatestOffsetResetPolicy(t *testing.T, kc *kubernetes.Clientset, data templateData) {
	t.Log("--- testing oneInvalidOffsetTopicWithLatestOffsetResetPolicy: scale out ---")
	data.Params = fmt.Sprintf("--topic %s --group %s --from-beginning", oneInvalidOffsetTopic, invalidOffsetGroup)
	data.Commit = StringTrue
	data.TopicName = oneInvalidOffsetTopic
	data.ResetPolicy = invalidOffsetGroup
	data.ScaleToZeroOnInvalid = StringFalse
	KubectlApplyWithTemplate(t, data, "singleDeploymentTemplate", singleDeploymentTemplate)
	defer KubectlDeleteWithTemplate(t, data, "singleDeploymentTemplate", singleDeploymentTemplate)
	KubectlApplyWithTemplate(t, data, "invalidOffsetWithLatestOffsetResetPolicyScaledObjectTemplate", invalidOffsetWithLatestOffsetResetPolicyScaledObjectTemplate)
	defer KubectlDeleteWithTemplate(t, data, "invalidOffsetWithLatestOffsetResetPolicyScaledObjectTemplate", invalidOffsetWithLatestOffsetResetPolicyScaledObjectTemplate)

	// Should scale to 1
	assert.True(t, WaitForDeploymentReplicaReadyCount(t, kc, deploymentName, testNamespace, 1, 60, 2),
		"replica count should be %d after 2 minute", 1)

	commitPartition(t, oneInvalidOffsetTopic, invalidOffsetGroup)
	publishMessage(t, oneInvalidOffsetTopic)

	// Should scale to 0
	assert.True(t, WaitForDeploymentReplicaReadyCount(t, kc, deploymentName, testNamespace, 0, 60, 10),
		"replica count should be %d after 10 minute", 0)
}

func testOneOnInvalidOffsetWithEarliestOffsetResetPolicy(t *testing.T, kc *kubernetes.Clientset, data templateData) {
	t.Log("--- testing oneInvalidOffsetTopicWithEarliestOffsetResetPolicy: scale out ---")
	data.Params = fmt.Sprintf("--topic %s --group %s --from-beginning", oneInvalidOffsetTopic, invalidOffsetGroup)
	data.Commit = StringTrue
	data.TopicName = oneInvalidOffsetTopic
	data.ResetPolicy = invalidOffsetGroup
	data.ScaleToZeroOnInvalid = StringFalse
	KubectlApplyWithTemplate(t, data, "singleDeploymentTemplate", singleDeploymentTemplate)
	defer KubectlDeleteWithTemplate(t, data, "singleDeploymentTemplate", singleDeploymentTemplate)
	publishMessage(t, oneInvalidOffsetTopic) // So that the latest offset is not 0
	KubectlApplyWithTemplate(t, data, "invalidOffsetWithEarliestOffsetResetPolicyScaledObjectTemplate", invalidOffsetWithEarliestOffsetResetPolicyScaledObjectTemplate)
	defer KubectlDeleteWithTemplate(t, data, "invalidOffsetWithEarliestOffsetResetPolicyScaledObjectTemplate", invalidOffsetWithEarliestOffsetResetPolicyScaledObjectTemplate)

	// Should scale to 1
	assert.True(t, WaitForDeploymentReplicaReadyCount(t, kc, deploymentName, testNamespace, 1, 60, 2),
		"replica count should be %d after 2 minute", 1)

	commitPartition(t, oneInvalidOffsetTopic, invalidOffsetGroup)
	publishMessage(t, oneInvalidOffsetTopic)

	// Should scale to 0
	assert.True(t, WaitForDeploymentReplicaReadyCount(t, kc, deploymentName, testNamespace, 0, 60, 10),
		"replica count should be %d after 10 minute", 0)
}

func publishMessage(t *testing.T, topic string) {
	_, _, err := ExecCommandOnSpecificPod(t, kafkaClientName, testNamespace, fmt.Sprintf(`echo "{"text": "foo"}" | kafka-console-producer --broker-list %s --topic %s`, bootstrapServer, topic))
	assert.NoErrorf(t, err, "cannot execute command - %s", err)
}

// publish a message to a specific partition; We can't specify the exact partition,
// but any messages with the same key will end up in the same partition
func publishMessagePartitionKey(t *testing.T, topic string, key string) {
	_, _, err := ExecCommandOnSpecificPod(t, kafkaClientName, testNamespace, fmt.Sprintf(`echo -e "%s\t {"text": "foo"}" | kafka-console-producer --property parse.key=true --broker-list %s --topic %s`, key, bootstrapServer, topic))
	assert.NoErrorf(t, err, "cannot execute command - %s", err)
}

func commitPartition(t *testing.T, topic string, group string) {
	_, _, err := ExecCommandOnSpecificPod(t, kafkaClientName, testNamespace, fmt.Sprintf(`kafka-console-consumer --bootstrap-server %s --topic %s --group %s --from-beginning --consumer-property enable.auto.commit=true --timeout-ms 15000`, bootstrapServer, topic, group))
	assert.NoErrorf(t, err, "cannot execute command - %s", err)
}

func testPersistentLag(t *testing.T, kc *kubernetes.Clientset, data templateData) {
	t.Log("--- testing  persistentLag: no scale out ---")

	// Simulate Consumption from topic by consumer group
	// To avoid edge case where scaling could be effectively disabled (Consumer never makes a commit)
	data.Params = fmt.Sprintf("--topic %s --group %s --from-beginning", persistentLagTopic, persistentLagGroup)
	data.Commit = StringTrue
	data.TopicName = persistentLagTopic
	data.ResetPolicy = persistentLagGroup
	data.ExcludePersistentLag = StringTrue
	KubectlApplyWithTemplate(t, data, "singleDeploymentTemplate", singleDeploymentTemplate)
	KubectlApplyWithTemplate(t, data, "persistentLagScaledObjectTemplate", persistentLagScaledObjectTemplate)

	// Scale application with kafka messages in persistentLagTopic
	publishMessage(t, persistentLagTopic)
	// TODO(jkyros): this test started flaking after the offset tests were added, but only on OpenShift. It's not
	// actually broken. From what I can tell, the published message gets snarfed off the queue almost immediately and we miss it here
	// if our polling interval is 2s (the upstream setting), but we always catch it if it's 1s.
	// Previously this persistent lag test ran immediately after testMultiTopic, and at the end of that test the deployment count was 2,
	// but now with the offset tests, the deployment starts at 0 and scales up, so somehow maybe we had either been passing this assertion
	// with the deployment from the previous case, or kafka was left in a different state with the old test than it does with the current test.
	// I'd love to refactor this to be more deterministic, but it will take time. For now, we set the polling interval to 1s and vow to come back
	// someday.
	assert.True(t, WaitForDeploymentReplicaReadyCount(t, kc, deploymentName, testNamespace, 1, 120, 1),
		"replica count should be %d after 2 minute", 1)
	// Recreate Deployment to deliberately assign different consumer group to deployment and scaled object
	// This is to simulate inability to consume from topic
	// Scaled Object remains unchanged
	KubernetesScaleDeployment(t, kc, deploymentName, 0, testNamespace)
	assert.True(t, WaitForPodsTerminated(t, kc, "app=kafka-consumer", testNamespace, 60, 2),
		"pod should be terminated after %d minute", 2)

	data.Params = fmt.Sprintf("--topic %s --group %s --from-beginning", persistentLagTopic, persistentLagDeploymentGroup)
	KubectlApplyWithTemplate(t, data, "singleDeploymentTemplate", singleDeploymentTemplate)

	messages := 5
	for i := 0; i < messages; i++ {
		publishMessage(t, persistentLagTopic)
	}

	// Persistent Lag should not scale pod above minimum replicas after 2 reconciliation cycles
	assert.True(t, WaitForDeploymentReplicaReadyCount(t, kc, deploymentName, testNamespace, 1, 60, 2),
		"replica count should be %d after 2 minute", 1)

	// Shouldn't scale pods
	AssertReplicaCountNotChangeDuringTimePeriod(t, kc, deploymentName, testNamespace, 1, 30)

	KubectlDeleteWithTemplate(t, data, "singleDeploymentTemplate", singleDeploymentTemplate)
	KubectlDeleteWithTemplate(t, data, "persistentLagScaledObjectTemplate", persistentLagScaledObjectTemplate)
}

func testScalingOnlyPartitionsWithLag(t *testing.T, kc *kubernetes.Clientset, data templateData) {
	t.Log("--- testing  limitToPartitionsWithLag: no scale out ---")

	// Simulate Consumption from topic by consumer group
	// To avoid edge case where scaling could be effectively disabled (Consumer never makes a commit)
	commitPartition(t, limitToPartitionsWithLagTopic, "latest")

	data.Params = fmt.Sprintf("--topic %s --group %s", limitToPartitionsWithLagTopic, limitToPartitionsWithLagGroup)
	data.Commit = StringFalse
	data.TopicName = limitToPartitionsWithLagTopic
	data.LimitToPartitionsWithLag = StringTrue
	data.ResetPolicy = "latest"

	KubectlApplyWithTemplate(t, data, "singleDeploymentTemplate", singleDeploymentTemplate)
	defer KubectlDeleteWithTemplate(t, data, "singleDeploymentTemplate", singleDeploymentTemplate)
	KubectlApplyWithTemplate(t, data, "limitToPartionsWithLagScaledObjectTemplate", limitToPartionsWithLagScaledObjectTemplate)
	defer KubectlDeleteWithTemplate(t, data, "limitToPartionsWithLagScaledObjectTemplate", limitToPartionsWithLagScaledObjectTemplate)

	// Shouldn't scale pods applying latest policy
	AssertReplicaCountNotChangeDuringTimePeriod(t, kc, deploymentName, testNamespace, 0, 30)

	// Scale application with kafka messages in persistentLagTopic
	firstPartitionKey := "my-first-key"

	// Shouldn't scale pods with only 1 message due to activation value
	publishMessagePartitionKey(t, limitToPartitionsWithLagTopic, firstPartitionKey)
	AssertReplicaCountNotChangeDuringTimePeriod(t, kc, deploymentName, testNamespace, 0, 30)

	// Publish 5 messages to the same partition
	messages := 5
	for i := 0; i < messages; i++ {
		publishMessagePartitionKey(t, limitToPartitionsWithLagTopic, firstPartitionKey)
	}

	assert.True(t, WaitForDeploymentReplicaReadyCount(t, kc, deploymentName, testNamespace, 1, 60, 2),
		"replica count should be %d after 2 minute", 1)

	// Partition lag should not scale pod above 1 replicas after 2 reconciliation cycles
	// because we only have lag on 1 partition
	AssertReplicaCountNotChangeDuringTimePeriod(t, kc, deploymentName, testNamespace, 1, 60)

	// publish new messages on a separate partition
	secondPartitionKey := "my-second-key"
	for i := 0; i < messages; i++ {
		publishMessagePartitionKey(t, limitToPartitionsWithLagTopic, secondPartitionKey)
	}

	assert.True(t, WaitForDeploymentReplicaReadyCount(t, kc, deploymentName, testNamespace, 2, 60, 2),
		"replica count should be %d after 2 minute", 2)

	// Partition lag should not scale pod above 2 replicas after 2 reconciliation cycles
	// because we only have lag on 2 partitions
	AssertReplicaCountNotChangeDuringTimePeriod(t, kc, deploymentName, testNamespace, 2, 60)
}

func addTopic(t *testing.T, data templateData, name string, partitions int) {
	t.Log("--- adding kafka topic" + name + " and partitions " + strconv.Itoa(partitions) + " ---")
	data.KafkaTopicName = name
	data.KafkaTopicPartitions = partitions
	KubectlApplyWithTemplate(t, data, "kafkaTopicTemplate", kafkaTopicTemplate)
	_, err := ExecuteCommand(fmt.Sprintf("kubectl wait kafkatopic/%s --for=condition=Ready --timeout=480s --namespace %s", name, testNamespace))
	require.NoErrorf(t, err, "cannot execute command - %s", err)
	t.Log("--- kafka topic added ---")
}

func addCluster(t *testing.T, data templateData) {
	t.Log("--- adding kafka cluster ---")
	KubectlApplyWithTemplate(t, data, "kafkaClusterTemplate", kafkaClusterTemplate)
	_, err := ExecuteCommand(fmt.Sprintf("kubectl wait kafka/%s --for=condition=Ready --timeout=480s --namespace %s", kafkaName, testNamespace))
	require.NoErrorf(t, err, "cannot execute command - %s", err)
	t.Log("--- kafka cluster added ---")
}

func getTemplateData() (templateData, []Template) {
	return templateData{
			TestNamespace:    testNamespace,
			DeploymentName:   deploymentName,
			KafkaName:        kafkaName,
			KafkaClientName:  kafkaClientName,
			BootstrapServer:  bootstrapServer,
			TopicName:        topic1,
			Topic1Name:       topic1,
			Topic2Name:       topic2,
			ResetPolicy:      "",
			ScaledObjectName: scaledObjectName,
		}, []Template{
			{Name: "kafkaClientTemplate", Config: kafkaClientTemplate},
		}
}
