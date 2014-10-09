package m_etcd

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-etcd/etcd"
)

const TestNameSpace = "/testnamespace"
const TestNodeID = "test-node01"

/*
	Running the Integration Test:
	#if you don't have etcd install use this script to set it up:
	sudo bash ./scripts/docker_run_etcd.sh

	PUBLIC_IP=`hostname --ip-address` ETCDTESTS=1 ETCDCTL_PEERS="${PUBLIC_IP}:5001,${PUBLIC_IP}:5002,${PUBLIC_IP}:5003" go test

*/

// Insure that Watch() picks up new tasks and returns them.
//
func TestCoordinatorTC1(t *testing.T) {
	skipEtcd(t)

	coordinator1, client := createEtcdCoordinator(t, TestNameSpace)
	defer coordinator1.Close()

	if coordinator1.TaskPath != TestNameSpace+"/tasks" {
		t.Fatalf("TestFailed: TaskPath should be \"/%s/tasks\" but we got \"%s\"", TestNameSpace, coordinator1.TaskPath)
	}

	coordinator1.Init(testLogger{"coordinator1", t})

	watchRes := make(chan string)
	task001 := "test-task0001"
	fullTask001Path := coordinator1.TaskPath + "/" + task001
	client.Delete(coordinator1.TaskPath+task001, true)
	go func() {
		//Watch blocks, so we need to test it in its own go routine.
		taskId, err := coordinator1.Watch()
		if err != nil {
			t.Fatalf("coordinator1.Watch() returned an err: %v", err)
		}
		t.Logf("We got a task id from the coordinator1.Watch() res:%s", taskId)
		watchRes <- taskId
	}()

	client.CreateDir(fullTask001Path, 1)

	select {
	case taskId := <-watchRes:
		if taskId != task001 {
			t.Fatalf("coordinator1.Watch() test failed: We received the incorrect taskId.  Got [%s] Expected[%s]", taskId, task001)
		}
	case <-time.After(time.Second * 5):
		t.Fatalf("coordinator1.Watch() test failed: The testcase timed out after 5 seconds.")
	}
}

//   Submit a task while a coordinator is actively watching for tasks.
//
func TestCoordinatorTC2(t *testing.T) {
	skipEtcd(t)

	coordinator1, eclient := createEtcdCoordinator(t, TestNameSpace)
	coordinator1.Init(testLogger{"coordinator1", t})

	test_finished := make(chan bool)
	testTasks := []string{"test-claiming-task0001", "test-claiming-task0002", "test-claiming-task0003"}

	mclient := NewClientWithLogger(TestNameSpace, eclient, testLogger{"metafora-client1", t})

	for _, taskId := range testTasks { //Remove any old taskids left over from other tests.
		err := mclient.DeleteTask(taskId)
		if err != nil {
			t.Logf("metafora client return an error trying to delete task. This is expected if the test cleaned up correctly. error:%v", err)
		}
	}

	startATaskWatcher := func() {
		//Watch blocks, so we need to test it in its own go routine.
		taskId, err := coordinator1.Watch()
		if err != nil {
			t.Fatalf("coordinator1.Watch() returned an err: %v", err)
		}

		t.Logf("We got a task id from the coordinator1.Watch() res: %s", taskId)

		if ok := coordinator1.Claim(taskId); !ok {
			t.Fatal("coordinator1.Claim() unable to claim the task")
		}

		test_finished <- true
	}

	go startATaskWatcher()
	time.Sleep(24 * time.Millisecond)
	err := mclient.SubmitTask(testTasks[0])
	if err != nil {
		t.Fatalf("Error submitting a task to metafora via the client.  Error:", err)
	}

	select {
	case res := <-test_finished:
		if !res {
			t.Fatalf("Background test checker failed so the test failed.")
		}
	case <-time.After(time.Second * 5):
		t.Fatalf("Test failed: The testcase timed out after 5 seconds.")
	}
}

//   1) Submit two tasks between calls to coordinator.Watch() to make sure the
//   coordinator picks up tasks made between requests to Watch().
//
//   2) Try claiming the same taskId twice.
//
func TestCoordinatorTC3(t *testing.T) {
	skipEtcd(t)

	coordinator1, eclient := createEtcdCoordinator(t, TestNameSpace)
	coordinator1.Init(testLogger{"coordinator1", t})
	coordinator2, _ := createEtcdCoordinator(t, TestNameSpace)
	coordinator2.Init(testLogger{"coordinator2", t})

	test_finished := make(chan bool)
	testTasks := []string{"test-claiming-task0001", "test-claiming-task0002", "test-claiming-task0003"}

	mclient := NewClientWithLogger(TestNameSpace, eclient, testLogger{"metafora-client1", t})

	for _, taskId := range testTasks { //Remove any old taskids left over from other tests.
		err := mclient.DeleteTask(taskId)
		if err != nil {
			t.Logf("metafora client return an error trying to delete task. This is expected if the test cleaned up correctly. error:%v", err)
		}
	}

	startATaskWatcher := func() {
		//Watch blocks, so we need to test it in its own go routine.
		taskId, err := coordinator1.Watch()
		if err != nil {
			t.Fatalf("coordinator1.Watch() returned an err: %v", err)
		}

		_, err = coordinator2.Watch() //coordinator2 should also pickup this task
		if err != nil {
			t.Fatalf("coordinator2.Watch() returned an err: %v", err)
		}

		t.Logf("We got a task id from the coordinator1.Watch() res: %s", taskId)

		if ok := coordinator1.Claim(taskId); !ok {
			t.Fatal("coordinator1.Claim() unable to claim the task")
		}

		//Try to claim the task in a second coordinator.  Should fail
		if ok := coordinator2.Claim(taskId); ok {
			t.Fatal("coordinator1.Claim() unable to claim the task")
		}

		test_finished <- true
	}

	err := mclient.SubmitTask(testTasks[1])
	if err != nil {
		t.Fatalf("Error submitting a task to metafora via the client.  Error:", err)
	}
	err = mclient.SubmitTask(testTasks[2])
	if err != nil {
		t.Fatalf("Error submitting a task to metafora via the client.  Error:", err)
	}

	go startATaskWatcher()
	select {
	case res := <-test_finished:
		if !res {
			t.Fatalf("Background test checker failed so the test failed.")
		}
	case <-time.After(time.Second * 5):
		t.Fatalf("Test failed: The testcase timed out after 5 seconds.")
	}
	go startATaskWatcher()
	select {
	case res := <-test_finished:
		if !res {
			t.Fatalf("Background test checker failed so the test failed.")
		}
	case <-time.After(time.Second * 5):
		t.Fatalf("Test failed: The testcase timed out after 5 seconds.")
	}
}

//   Submit a task before any coordinator is active and watching for tasks.
//
func TestCoordinatorTC4(t *testing.T) {
	skipEtcd(t)
	t.Log("\nDoing Test Setup")

	test_finished := make(chan bool)
	testTasks := []string{"testtask0001", "testtask0002", "testtask0003"}

	eclient := createEtcdClient(t)
	mclient := NewClientWithLogger(TestNameSpace, eclient, testLogger{"metafora-client1", t})
	/*
		for _, taskId := range testTasks { //Remove any old taskids left over from other tests.
			err := mclient.DeleteTask(taskId)
			if err != nil {
				t.Logf("metafora client return an error trying to delete task. This is expected if the test cleaned up correctly. error:%v", err)
			}
		}
	*/
	t.Log("\n\nStart of the test case")
	err := mclient.SubmitTask(testTasks[0])
	if err != nil {
		//t.Fatalf("%s Error submitting a task to metafora via the client.  Error:", err)
	}

	const sorted = false
	const recursive = true
	fmt.Println("before\n\n")
	eclient.Get("/testnamespace/", sorted, recursive)

	//Don't start up the coordinator until after the metafora client has submitted work.
	coordinator1, _ := createEtcdCoordinator(t, TestNameSpace)
	coordinator1.Init(testLogger{"coordinator1", t})
	fmt.Println("after\n\n")
	eclient.Get("/testnamespace/", sorted, recursive)

	startATaskWatcher := func() {
		//Watch blocks, so we need to test it in its own go routine.
		taskId, err := coordinator1.Watch()
		if err != nil {
			t.Fatalf("coordinator1.Watch() returned an err: %v", err)
		}

		t.Logf("We got a task id from the coordinator1.Watch() res: %s", taskId)

		if ok := coordinator1.Claim(taskId); !ok {
			t.Fatal("coordinator1.Claim() unable to claim the task")
		}

		test_finished <- true
	}

	go startATaskWatcher()

	select {
	case res := <-test_finished:
		if !res {
			t.Fatalf("Background test checker failed so the test failed.")
		}
	case <-time.After(time.Second * 5):
		t.Fatalf("Test failed: The testcase timed out after 5 seconds.")
	}
}

func createEtcdClient(t *testing.T) *etcd.Client {
	peerAddrs := os.Getenv("ETCDCTL_PEERS") //This is the same ENV that etcdctl uses for Peers.
	if peerAddrs == "" {
		peerAddrs = "localhost:5001,localhost:5002,localhost:5003"
	}

	peers := strings.Split(peerAddrs, ",")

	client := etcd.NewClient(peers)

	ok := client.SyncCluster()

	if !ok {
		t.Fatalf("Cannot sync with the cluster using peers " + strings.Join(peers, ", "))
	}

	if !isEtcdUp(client, t) {
		t.Fatalf("While testing etcd, the test couldn't connect to etcd. " + strings.Join(peers, ", "))
	}

	return client
}

func createEtcdCoordinator(t *testing.T, namespace string) (*EtcdCoordinator, *etcd.Client) {
	client := createEtcdClient(t)
	const recursive = true
	client.Delete(namespace, recursive)

	return NewEtcdCoordinator(TestNodeID, namespace, client).(*EtcdCoordinator), client
}

func isEtcdUp(client *etcd.Client, t *testing.T) bool {
	client.Create("/foo", "test", 1)
	res, err := client.Get("/foo", false, false)
	if err != nil {
		t.Errorf("Writing a test key to etcd failed. error:%v", err)
		return false
	} else {
		t.Log(fmt.Sprintf("Res:[Action:%s Key:%s Value:%s tll:%d]", res.Action, res.Node.Key, res.Node.Value, res.Node.TTL))
		return true
	}
}
