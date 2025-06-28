package main

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/rivo/tview"
)

// MockECSClient implements the ECSAPI interface for testing.
type MockECSClient struct {
	ListClustersFunc       func(ctx context.Context, params *ecs.ListClustersInput, optFns ...func(*ecs.Options)) (*ecs.ListClustersOutput, error)
	ListServicesFunc       func(ctx context.Context, params *ecs.ListServicesInput, optFns ...func(*ecs.Options)) (*ecs.ListServicesOutput, error)
	ListTasksFunc          func(ctx context.Context, params *ecs.ListTasksInput, optFns ...func(*ecs.Options)) (*ecs.ListTasksOutput, error)
	DescribeTasksFunc      func(ctx context.Context, params *ecs.DescribeTasksInput, optFns ...func(*ecs.Options)) (*ecs.DescribeTasksOutput, error)
	DescribeTaskDefinitionFunc func(ctx context.Context, params *ecs.DescribeTaskDefinitionInput, optFns ...func(*ecs.Options)) (*ecs.DescribeTaskDefinitionOutput, error)
	StopTaskFunc           func(ctx context.Context, params *ecs.StopTaskInput, optFns ...func(*ecs.Options)) (*ecs.StopTaskOutput, error)
	RunTaskFunc            func(ctx context.Context, params *ecs.RunTaskInput, optFns ...func(*ecs.Options)) (*ecs.RunTaskOutput, error)
}

func (m *MockECSClient) ListClusters(ctx context.Context, params *ecs.ListClustersInput, optFns ...func(*ecs.Options)) (*ecs.ListClustersOutput, error) {
	return m.ListClustersFunc(ctx, params, optFns...)
}
func (m *MockECSClient) ListServices(ctx context.Context, params *ecs.ListServicesInput, optFns ...func(*ecs.Options)) (*ecs.ListServicesOutput, error) {
	return m.ListServicesFunc(ctx, params, optFns...)
}
func (m *MockECSClient) ListTasks(ctx context.Context, params *ecs.ListTasksInput, optFns ...func(*ecs.Options)) (*ecs.ListTasksOutput, error) {
	return m.ListTasksFunc(ctx, params, optFns...)
}
func (m *MockECSClient) DescribeTasks(ctx context.Context, params *ecs.DescribeTasksInput, optFns ...func(*ecs.Options)) (*ecs.DescribeTasksOutput, error) {
	return m.DescribeTasksFunc(ctx, params, optFns...)
}
func (m *MockECSClient) DescribeTaskDefinition(ctx context.Context, params *ecs.DescribeTaskDefinitionInput, optFns ...func(*ecs.Options)) (*ecs.DescribeTaskDefinitionOutput, error) {
	return m.DescribeTaskDefinitionFunc(ctx, params, optFns...)
}
func (m *MockECSClient) StopTask(ctx context.Context, params *ecs.StopTaskInput, optFns ...func(*ecs.Options)) (*ecs.StopTaskOutput, error) {
	return m.StopTaskFunc(ctx, params, optFns...)
}
func (m *MockECSClient) RunTask(ctx context.Context, params *ecs.RunTaskInput, optFns ...func(*ecs.Options)) (*ecs.RunTaskOutput, error) {
	return m.RunTaskFunc(ctx, params, optFns...)
}

// MockCloudWatchLogsClient implements the CloudWatchLogsAPI interface for testing.
type MockCloudWatchLogsClient struct {
	GetLogEventsFunc func(ctx context.Context, params *cloudwatchlogs.GetLogEventsInput, optFns ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.GetLogEventsOutput, error)
}

func (m *MockCloudWatchLogsClient) GetLogEvents(ctx context.Context, params *cloudwatchlogs.GetLogEventsInput, optFns ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.GetLogEventsOutput, error) {
	return m.GetLogEventsFunc(ctx, params, optFns...)
}

func TestLoadClusters(t *testing.T) {
	// Create a mock ECS client
	mockECS := &MockECSClient{
		ListClustersFunc: func(ctx context.Context, params *ecs.ListClustersInput, optFns ...func(*ecs.Options)) (*ecs.ListClustersOutput, error) {
			return &ecs.ListClustersOutput{
				ClusterArns: []string{
					"arn:aws:ecs:us-east-1:123456789012:cluster/test-cluster-1",
					"arn:aws:ecs:us-east-1:123456789012:cluster/test-cluster-2",
				},
			}, nil
		},
	}

	// Create a mock CloudWatch Logs client (not used in this test, but needed for EcsClient)
	mockLogs := &MockCloudWatchLogsClient{}

	// Create an EcsClient with the mocks
	ecsClient := &EcsClient{
		client: mockECS,
		logs:   mockLogs,
	}

	// Create the App with the mocked EcsClient
	app := &App{
		app:   tview.NewApplication(),
		pages: tview.NewPages(),
		ecs:   ecsClient,
	}
	app.setupUI()

	// Directly call loadClusters and then process UI updates
	app.loadClusters()

	// Process all pending UI updates
	app.app.Draw()

	// Assertions
	if app.clustersList.GetItemCount() != 2 {
		t.Errorf("Expected 2 clusters, got %d", app.clustersList.GetItemCount())
	}

	mainText, secondaryText := app.clustersList.GetItemText(0)
	if mainText != "test-cluster-1" || secondaryText != "arn:aws:ecs:us-east-1:123456789012:cluster/test-cluster-1" {
		t.Errorf("Unexpected item 0: %s, %s", mainText, secondaryText)
	}

	mainText, secondaryText = app.clustersList.GetItemText(1)
	if mainText != "test-cluster-2" || secondaryText != "arn:aws:ecs:us-east-1:123456789012:cluster/test-cluster-2" {
		t.Errorf("Unexpected item 1: %s, %s", mainText, secondaryText)
	}
}

func TestRestartTask(t *testing.T) {
	// Mock data
	clusterArn := "arn:aws:ecs:us-east-1:123456789012:cluster/test-cluster"
	taskArn := "arn:aws:ecs:us-east-1:1234567890abcdef1234567890abcdef"
	taskDefArn := "arn:aws:ecs:us-east-1:123456789012:task-definition/test-task-def:1"

	// Create a mock ECS client
	mockECS := &MockECSClient{
		DescribeTasksFunc: func(ctx context.Context, params *ecs.DescribeTasksInput, optFns ...func(*ecs.Options)) (*ecs.DescribeTasksOutput, error) {
			return &ecs.DescribeTasksOutput{
				Tasks: []types.Task{
					{
						TaskArn:        &taskArn,
						TaskDefinitionArn: &taskDefArn,
						LaunchType:     types.LaunchTypeFargate,
						PlatformVersion: aws.String("1.4.0"),
					},
				},
			}, nil
		},
		StopTaskFunc: func(ctx context.Context, params *ecs.StopTaskInput, optFns ...func(*ecs.Options)) (*ecs.StopTaskOutput, error) {
			// Assert that StopTask is called with the correct parameters
			if *params.Cluster != clusterArn || *params.Task != taskArn {
				t.Errorf("StopTask called with incorrect parameters: cluster=%s, task=%s", *params.Cluster, *params.Task)
			}
			return &ecs.StopTaskOutput{}, nil
		},
		RunTaskFunc: func(ctx context.Context, params *ecs.RunTaskInput, optFns ...func(*ecs.Options)) (*ecs.RunTaskOutput, error) {
			// Assert that RunTask is called with the correct parameters
			if *params.Cluster != clusterArn || *params.TaskDefinition != taskDefArn || params.LaunchType != types.LaunchTypeFargate || *params.Count != 1 {
				t.Errorf("RunTask called with incorrect parameters: cluster=%s, taskDef=%s, launchType=%s, count=%d", *params.Cluster, *params.TaskDefinition, params.LaunchType, *params.Count)
			}
			// For simplicity, we're not fully validating NetworkConfiguration here.
			return &ecs.RunTaskOutput{}, nil
		},
	}

	// Create a mock CloudWatch Logs client (not used in this test)
	mockLogs := &MockCloudWatchLogsClient{}

	// Create an EcsClient with the mocks
	ecsClient := &EcsClient{
		client: mockECS,
		logs:   mockLogs,
	}

	// Call the RestartTask method
	err := ecsClient.RestartTask(context.Background(), clusterArn, taskArn)
	if err != nil {
		t.Fatalf("RestartTask failed: %v", err)
	}
}