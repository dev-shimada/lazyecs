package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const awsTimeout = 30 * time.Second

// --- AWS API Interfaces ---
// This makes the client testable by allowing us to mock AWS services.

type ECSAPI interface {
	ListClusters(ctx context.Context, params *ecs.ListClustersInput, optFns ...func(*ecs.Options)) (*ecs.ListClustersOutput, error)
	ListServices(ctx context.Context, params *ecs.ListServicesInput, optFns ...func(*ecs.Options)) (*ecs.ListServicesOutput, error)
	ListTasks(ctx context.Context, params *ecs.ListTasksInput, optFns ...func(*ecs.Options)) (*ecs.ListTasksOutput, error)
	DescribeTasks(ctx context.Context, params *ecs.DescribeTasksInput, optFns ...func(*ecs.Options)) (*ecs.DescribeTasksOutput, error)
	DescribeTaskDefinition(ctx context.Context, params *ecs.DescribeTaskDefinitionInput, optFns ...func(*ecs.Options)) (*ecs.DescribeTaskDefinitionOutput, error)
	StopTask(ctx context.Context, params *ecs.StopTaskInput, optFns ...func(*ecs.Options)) (*ecs.StopTaskOutput, error)
	RunTask(ctx context.Context, params *ecs.RunTaskInput, optFns ...func(*ecs.Options)) (*ecs.RunTaskOutput, error)
}

type CloudWatchLogsAPI interface {
	GetLogEvents(ctx context.Context, params *cloudwatchlogs.GetLogEventsInput, optFns ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.GetLogEventsOutput, error)
}

// EcsClient is a wrapper for the AWS clients.
type EcsClient struct {
	client ECSAPI
	logs   CloudWatchLogsAPI
}

// NewEcsClient creates a new, real ECS client for the application.
func NewEcsClient() (*EcsClient, error) {
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		return nil, fmt.Errorf("unable to load SDK config: %w", err)
	}
	return &EcsClient{
		client: ecs.NewFromConfig(cfg),
		logs:   cloudwatchlogs.NewFromConfig(cfg),
	}, nil
}

// --- EcsClient Methods ---

func (c *EcsClient) StopTask(ctx context.Context, clusterArn string, taskArn string) error {
	_, err := c.client.StopTask(ctx, &ecs.StopTaskInput{
		Cluster: &clusterArn,
		Task:    &taskArn,
		Reason:  aws.String("Stopped by lazyecs"),
	})
	if err != nil {
		return fmt.Errorf("failed to stop task %s: %w", taskArn, err)
	}
	return nil
}

// RestartTask stops an existing task and runs a new one with the same task definition.
func (c *EcsClient) RestartTask(ctx context.Context, clusterArn string, taskArn string) error {
	// 1. Get task details to find the task definition ARN and network configuration
	task, err := c.GetTaskDetails(ctx, clusterArn, taskArn)
	if err != nil {
		return fmt.Errorf("failed to get task details for restart: %w", err)
	}

	if task.TaskDefinitionArn == nil {
		return fmt.Errorf("task definition ARN not found for task %s", taskArn)
	}

	// 2. Stop the existing task
	err = c.StopTask(ctx, clusterArn, taskArn)
	if err != nil {
		return fmt.Errorf("failed to stop task %s for restart: %w", taskArn, err)
	}

	// 3. Prepare RunTaskInput
	runTaskInput := &ecs.RunTaskInput{
		Cluster:        &clusterArn,
		TaskDefinition: task.TaskDefinitionArn,
		LaunchType:     task.LaunchType,
		Count:          aws.Int32(1),
	}

	// Handle Fargate specific parameters like PlatformVersion and NetworkConfiguration
	if task.LaunchType == types.LaunchTypeFargate {
		runTaskInput.PlatformVersion = task.PlatformVersion

		// For simplicity, we are not attempting to copy network configuration from the old task.
		// In a real scenario, you would need to retrieve and re-apply the network configuration
		// (subnets, security groups) from the task's attachments or the task definition.
		// This is a known simplification for the purpose of this exercise.
	}

	// 4. Run a new task with the same task definition
	_, err = c.client.RunTask(ctx, runTaskInput)
	if err != nil {
		return fmt.Errorf("failed to run new task for restart: %w", err)
	}

	return nil
}

func (c *EcsClient) GetClusters(ctx context.Context) ([]string, error) {
	result, err := c.client.ListClusters(ctx, &ecs.ListClustersInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to list clusters: %w", err)
	}
	return result.ClusterArns, nil
}

func (c *EcsClient) GetServices(ctx context.Context, clusterArn string) ([]string, error) {
	result, err := c.client.ListServices(ctx, &ecs.ListServicesInput{
		Cluster: &clusterArn,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list services for cluster %s: %w", clusterArn, err)
	}
	return result.ServiceArns, nil
}

func (c *EcsClient) GetTasks(ctx context.Context, clusterArn string, serviceArn string) ([]string, error) {
	parts := strings.Split(serviceArn, "/")
	serviceName := parts[len(parts)-1]
	result, err := c.client.ListTasks(ctx, &ecs.ListTasksInput{
		Cluster:     &clusterArn,
		ServiceName: &serviceName,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list tasks for service %s: %w", serviceName, err)
	}
	return result.TaskArns, nil
}

func (c *EcsClient) GetTaskDetails(ctx context.Context, clusterArn string, taskArn string) (*types.Task, error) {
	result, err := c.client.DescribeTasks(ctx, &ecs.DescribeTasksInput{
		Cluster: &clusterArn,
		Tasks:   []string{taskArn},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to describe task %s: %w", taskArn, err)
	}
	if len(result.Tasks) == 0 {
		return nil, fmt.Errorf("task not found: %s", taskArn)
	}
	return &result.Tasks[0], nil
}

func (c *EcsClient) GetTaskDefinition(ctx context.Context, taskDefArn string) (*types.TaskDefinition, error) {
	result, err := c.client.DescribeTaskDefinition(ctx, &ecs.DescribeTaskDefinitionInput{
		TaskDefinition: &taskDefArn,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to describe task definition %s: %w", taskDefArn, err)
	}
	return result.TaskDefinition, nil
}

func (c *EcsClient) GetLogs(ctx context.Context, logGroupName string, logStreamName string) ([]string, error) {
	startFromHead := true
	result, err := c.logs.GetLogEvents(ctx, &cloudwatchlogs.GetLogEventsInput{
		LogGroupName:  &logGroupName,
		LogStreamName: &logStreamName,
		StartFromHead: &startFromHead,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get log events: %w", err)
	}

	var logs []string
	for _, event := range result.Events {
		logs = append(logs, *event.Message)
	}
	return logs, nil
}

// --- TUI Application ---

type App struct {
	app   *tview.Application
	pages *tview.Pages
	ecs   *EcsClient

	// UI components
	clustersList *tview.List
	servicesList *tview.List
	tasksList    *tview.List
	mainContent  *tview.TextView
	flexLayout   *tview.Flex

	// State
	selectedClusterArn string
	selectedServiceArn string
	selectedTaskArn    string
}

// NewApp creates a new application with a real EcsClient.
func NewApp() (*App, error) {
	ecsClient, err := NewEcsClient()
	if err != nil {
		return nil, err
	}
	return newAppWithClient(tview.NewApplication(), tview.NewPages(), ecsClient), nil
}

// newAppWithClient creates a new application with a given EcsClient (used for testing).
func newAppWithClient(app *tview.Application, pages *tview.Pages, ecsClient *EcsClient) *App {
	a := &App{
		app:   app,
		pages: pages,
		ecs:   ecsClient,
	}
	a.setupUI()
	return a
}

func (a *App) setupUI() {
	a.clustersList = tview.NewList().ShowSecondaryText(false)
	a.clustersList.SetBorder(true).SetTitle("Clusters")
	a.clustersList.SetSelectedFunc(func(index int, mainText, secondaryText string, shortcut rune) {
		a.selectedClusterArn = secondaryText
		a.loadServices(secondaryText)
	})

	a.servicesList = tview.NewList().ShowSecondaryText(false)
	a.servicesList.SetBorder(true).SetTitle("Services")
	a.servicesList.SetSelectedFunc(func(index int, mainText, secondaryText string, shortcut rune) {
		a.selectedServiceArn = secondaryText
		a.loadTasks(a.selectedClusterArn, secondaryText)
	})

	a.tasksList = tview.NewList().ShowSecondaryText(false)
	a.tasksList.SetBorder(true).SetTitle("Tasks")
	a.tasksList.SetSelectedFunc(func(index int, mainText, secondaryText string, shortcut rune) {
		a.selectedTaskArn = secondaryText
		a.loadTaskDetails(a.selectedClusterArn, secondaryText)
	})

	a.mainContent = tview.NewTextView().
		SetDynamicColors(true).
		SetRegions(true).
		SetWordWrap(true)
	a.mainContent.SetBorder(true).SetTitle("Content (d: details, l: logs, s: stop, r: restart)")

	leftPane := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.clustersList, 0, 1, true).
		AddItem(a.servicesList, 0, 1, false).
		AddItem(a.tasksList, 0, 1, false)

	a.flexLayout = tview.NewFlex().
		AddItem(leftPane, 0, 1, true).
		AddItem(a.mainContent, 0, 3, false)

	a.pages.AddPage("main", a.flexLayout, true, true)

	a.app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Rune() {
		case 'q':
			a.app.Stop()
		case 'd':
			if a.selectedTaskArn != "" {
				a.loadTaskDetails(a.selectedClusterArn, a.selectedTaskArn)
			}
		case 'l':
			if a.selectedTaskArn != "" {
				a.loadLogs(a.selectedClusterArn, a.selectedTaskArn)
			}
		case 's':
			if a.selectedTaskArn != "" {
				a.showStopTaskModal()
			}
		case 'r':
			if a.selectedTaskArn != "" {
				a.showRestartTaskModal()
			}
		}
		return event
	})
}

func (a *App) showStopTaskModal() {
	modal := tview.NewModal().
		SetText(fmt.Sprintf("Are you sure you want to stop task %s?", a.selectedTaskArn)).
		AddButtons([]string{"Yes", "No"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			if buttonLabel == "Yes" {
				a.stopTask(a.selectedClusterArn, a.selectedTaskArn)
			}
			a.pages.RemovePage("stopModal")
		})
	a.pages.AddPage("stopModal", modal, true, true)
}

func (a *App) stopTask(clusterArn, taskArn string) {
	a.app.QueueUpdateDraw(func() {
		a.mainContent.SetText(fmt.Sprintf("Stopping task %s...", taskArn))
	})

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), awsTimeout)
		defer cancel()

		err := a.ecs.StopTask(ctx, clusterArn, taskArn)
		a.app.QueueUpdateDraw(func() {
			if err != nil {
				a.mainContent.SetText(fmt.Sprintf("[red]Error stopping task: %v", err))
			} else {
				a.mainContent.SetText(fmt.Sprintf("[green]Task %s stopped successfully.", taskArn))
				time.AfterFunc(2*time.Second, func() {
					a.loadTasks(a.selectedClusterArn, a.selectedServiceArn)
				})
			}
		})
	}()
}

func (a *App) showRestartTaskModal() {
	modal := tview.NewModal().
		SetText(fmt.Sprintf("Are you sure you want to restart task %s?", a.selectedTaskArn)).
		AddButtons([]string{"Yes", "No"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			if buttonLabel == "Yes" {
				a.restartTask(a.selectedClusterArn, a.selectedTaskArn)
			}
			a.pages.RemovePage("restartModal")
		})
	a.pages.AddPage("restartModal", modal, true, true)
}

func (a *App) restartTask(clusterArn, taskArn string) {
		a.app.QueueUpdateDraw(func() {
		a.mainContent.SetText(fmt.Sprintf("Restarting task %s...", taskArn))
	})

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), awsTimeout)
		defer cancel()

		err := a.ecs.RestartTask(ctx, clusterArn, taskArn)
		a.app.QueueUpdateDraw(func() {
			if err != nil {
				a.mainContent.SetText(fmt.Sprintf("[red]Error restarting task: %v", err))
			} else {
				a.mainContent.SetText(fmt.Sprintf("[green]Task %s restarted successfully.", taskArn))
				time.AfterFunc(2*time.Second, func() {
					a.loadTasks(a.selectedClusterArn, a.selectedServiceArn)
				})
			}
		})
	}()
}

func (a *App) loadClusters() {
	a.app.QueueUpdateDraw(func() {
		a.clustersList.Clear()
		a.clustersList.AddItem("Loading...", "", 0, nil)
	})

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), awsTimeout)
		defer cancel()

		clusters, err := a.ecs.GetClusters(ctx)
		a.app.QueueUpdateDraw(func() {
			a.clustersList.Clear()
			if err != nil {
				a.mainContent.SetText(fmt.Sprintf("[red]Error: %v", err))
				return
			}
			if len(clusters) == 0 {
				a.clustersList.AddItem("No clusters found", "", 0, nil)
			} else {
				for _, clusterArn := range clusters {
					parts := strings.Split(clusterArn, "/")
					name := parts[len(parts)-1]
					a.clustersList.AddItem(name, clusterArn, 0, nil)
				}
			}
		})
	}()
}

func (a *App) loadServices(clusterArn string) {
	if clusterArn == "" {
		return
	}

	a.app.QueueUpdateDraw(func() {
		a.servicesList.Clear()
		a.tasksList.Clear()
		a.mainContent.Clear()
		a.servicesList.AddItem("Loading...", "", 0, nil)
	})

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), awsTimeout)
		defer cancel()

		services, err := a.ecs.GetServices(ctx, clusterArn)
		a.app.QueueUpdateDraw(func() {
			a.servicesList.Clear()
			if err != nil {
				a.mainContent.SetText(fmt.Sprintf("[red]Error: %v", err))
				return
			}
			if len(services) == 0 {
				a.servicesList.AddItem("No services found", "", 0, nil)
			} else {
				for _, serviceArn := range services {
					parts := strings.Split(serviceArn, "/")
					name := parts[len(parts)-1]
					a.servicesList.AddItem(name, serviceArn, 0, nil)
				}
			}
		})
	}()
}

func (a *App) loadTasks(clusterArn string, serviceArn string) {
	if clusterArn == "" || serviceArn == "" {
		return
	}

	a.app.QueueUpdateDraw(func() {
		a.tasksList.Clear()
		a.mainContent.Clear()
		a.tasksList.AddItem("Loading...", "", 0, nil)
	})

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), awsTimeout)
		defer cancel()

		tasks, err := a.ecs.GetTasks(ctx, clusterArn, serviceArn)
		a.app.QueueUpdateDraw(func() {
			a.tasksList.Clear()
			if err != nil {
				a.mainContent.SetText(fmt.Sprintf("[red]Error: %v", err))
				return
			}
			if len(tasks) == 0 {
				a.tasksList.AddItem("No tasks found", "", 0, nil)
			} else {
				for _, taskArn := range tasks {
					parts := strings.Split(taskArn, "/")
					name := parts[len(parts)-1]
					a.tasksList.AddItem(name, taskArn, 0, nil)
				}
			}
		})
	}()
}

func (a *App) loadTaskDetails(clusterArn string, taskArn string) {
	if clusterArn == "" || taskArn == "" {
		return
	}

	a.app.QueueUpdateDraw(func() {
		a.mainContent.SetText("Loading task details...")
	})

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), awsTimeout)
		defer cancel()

		task, err := a.ecs.GetTaskDetails(ctx, clusterArn, taskArn)
		a.app.QueueUpdateDraw(func() {
			if err != nil {
				a.mainContent.SetText(fmt.Sprintf("[red]Error: %v", err))
				return
			}
			var builder strings.Builder
			builder.WriteString(fmt.Sprintf("[yellow]Task ARN: [white]%s\n", *task.TaskArn))
			builder.WriteString(fmt.Sprintf("[yellow]Task Definition: [white]%s\n", *task.TaskDefinitionArn))
			builder.WriteString(fmt.Sprintf("[yellow]Cluster: [white]%s\n", *task.ClusterArn))
			builder.WriteString(fmt.Sprintf("[yellow]Last Status: [white]%s\n", *task.LastStatus))
			builder.WriteString(fmt.Sprintf("[yellow]Desired Status: [white]%s\n", *task.DesiredStatus))
			builder.WriteString(fmt.Sprintf("[yellow]CPU: [white]%s\n", *task.Cpu))
			builder.WriteString(fmt.Sprintf("[yellow]Memory: [white]%s\n", *task.Memory))
			builder.WriteString(fmt.Sprintf("[yellow]Launch Type: [white]%s\n", task.LaunchType))

			builder.WriteString("\n[yellow]Containers:\n")
			for _, container := range task.Containers {
				builder.WriteString(fmt.Sprintf("  [green]Name: [white]%s\n", *container.Name))
				builder.WriteString(fmt.Sprintf("    [cyan]Image: [white]%s\n", *container.Image))
				builder.WriteString(fmt.Sprintf("    [cyan]Status: [white]%s\n", *container.LastStatus))
				if container.ExitCode != nil {
					builder.WriteString(fmt.Sprintf("    [cyan]Exit Code: [white]%d\n", *container.ExitCode))
				}
			}

			a.mainContent.SetText(builder.String())
		})
	}()
}

func (a *App) loadLogs(clusterArn string, taskArn string) {
	if clusterArn == "" || taskArn == "" {
		return
	}

	a.app.QueueUpdateDraw(func() {
		a.mainContent.SetText("Loading logs...")
	})

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), awsTimeout)
		defer cancel()

		// 1. Get Task Details to find the Task Definition ARN
		task, err := a.ecs.GetTaskDetails(ctx, clusterArn, taskArn)
		if err != nil {
			a.app.QueueUpdateDraw(func() { a.mainContent.SetText(fmt.Sprintf("[red]Error getting task details: %v", err)) })
			return
		}

		// 2. Get Task Definition to find log configuration
		taskDef, err := a.ecs.GetTaskDefinition(ctx, *task.TaskDefinitionArn)
		if err != nil {
			a.app.QueueUpdateDraw(func() { a.mainContent.SetText(fmt.Sprintf("[red]Error getting task definition: %v", err)) })
			return
		}

		// For simplicity, we'll just use the first container's log configuration.
		if len(taskDef.ContainerDefinitions) == 0 {
			a.app.QueueUpdateDraw(func() { a.mainContent.SetText("[red]No container definitions found in task.") })
			return
		}
		containerDef := taskDef.ContainerDefinitions[0]
		logConfig := containerDef.LogConfiguration
		if logConfig == nil || logConfig.LogDriver != types.LogDriverAwslogs {
			a.app.QueueUpdateDraw(func() { a.mainContent.SetText("[yellow]Log configuration not found or not 'awslogs'.") })
			return
		}

		// 3. Construct the log stream name and get logs
		logGroupName := logConfig.Options["awslogs-group"]
		logStreamPrefix := logConfig.Options["awslogs-stream-prefix"]
		taskId := strings.Split(taskArn, "/")[2]
		containerName := *containerDef.Name
		logStreamName := fmt.Sprintf("%s/%s/%s", logStreamPrefix, containerName, taskId)

		logs, err := a.ecs.GetLogs(ctx, logGroupName, logStreamName)
		a.app.QueueUpdateDraw(func() {
			if err != nil {
				a.mainContent.SetText(fmt.Sprintf("[red]Error getting logs: %v", err))
				return
			}
			if len(logs) == 0 {
				a.mainContent.SetText(fmt.Sprintf("No logs found for stream: %s", logStreamName))
			} else {
				a.mainContent.SetText(strings.Join(logs, "\n"))
			}
		})
	}()
}

// Run starts the application.
func (a *App) Run() error {
	go a.loadClusters()
	return a.app.SetRoot(a.pages, true).Run()
}

func main() {
	app, err := NewApp()
	if err != nil {
		log.Fatalf("Failed to create app: %v", err)
	}

	if err := app.Run(); err != nil {
		log.Fatalf("Failed to run app: %v", err)
	}
}
