# Frequent Code Review Comments

## Table of Contents

<!-- toc -->
- [Naming style should be consistent](#naming-style-should-be-consistent)
- [Name utility functions generically](#name-utility-functions-generically)
- [Export names only if you must](#export-names-only-if-you-must)
- [Organize imports in groups](#organize-imports-in-groups)
- [Do not capitalize Errors](#do-not-capitalize-errors)
- [Handle the error or return it](#handle-the-error-or-return-it)
- [Utility functions should just return error if there is one](#utility-functions-should-just-return-error-if-there-is-one)
- [Logging convention](#logging-convention)
- [Do not make all logs default level](#do-not-make-all-logs-default-level)
- [Use same level for similar logs](#use-same-level-for-similar-logs)
- [Mutex and RWMutex](#mutex-and-rwmutex)
- [Shared resource creation](#shared-resource-creation)
- [Implement controller properly](#implement-controller-properly)
- [Return concrete types](#return-concrete-types)
- [Use efficient data structure](#use-efficient-data-structure)
- [Use Indexer properly](#use-indexer-properly)
- [Table driven test](#table-driven-test)
- [Use mocking for unit tests](#use-mocking-for-unit-tests)
- [Commit Message](#commit-message)
<!-- /toc -->

## Naming style should be consistent

Name similar functions, variables in a consistent style, to make the names
predictable when searching and to indicate their correlation by names.

```go
// Bad examples
func getInterfaceByName(name string) (*Interface, bool) { ... }

func getIntfByIP(ip string) (*Interface, bool) { ... }

func createAddressGroup(group *Group) (string, error) { ... }

func updateAG(g *Group) error { ... }

// Good examples
func getInterfaceByName(name string) (*Interface, bool) { ... }

func getInterfaceByIP(ip string) (*Interface, bool) { ... }

func createAddressGroup(group *Group) (string, error) { ... }

func updateAddressGroup(group *Group) error { ... }
```

## Name utility functions generically
Do not name utility function too specific to business code, to avoid redundant
code or confusing usage.

```go
// Bad examples
func GetUplinkIPNetByName(name string) net.IP { ... }

func RenameUplinkInterface(fromName, toName string) error { ... }

// Good examples
func GetIPNetByName(name string) net.IP { ... }

func RenameInterface(fromName, toName string) error { ... }
```

## Export names only if you must
Variables, fields, functions, structs and interfaces should not be exported
(capitalized) unless they are intended to be consumed externally, to make code
loose coupled, easier to maintain.

```go
// Bad examples
type RouteClient struct {
	// Introduce a risk that the cache could be accessed externally which may
	// lead to race condition, data inconsistency. 
	// Can not change data structure of RouteCache easily.
	RouteCache map[string]Route
}

// Good examples
type RouteClient struct {
	routeCache map[string]*Route
}

func GetRoute(destination string) *Route { ... }
```

## Organize imports in groups

Imports should be organized in groups (the standard library packages, the third
party packages, the local packages), with blank lines between them. 

```go
// Bad examples
import (
	"fmt"
	"github.com/vishvananda/netlink"
	"os"
	
	"antrea.io/antrea/pkg/agent/util"
	"k8s.io/klog/v2"
)

// Good examples
import (
	"fmt"
	"os"
	
	"github.com/vishvananda/netlink"
	"k8s.io/klog/v2"
	
	"antrea.io/antrea/pkg/agent/util"
)
```

## Do not capitalize Errors

Error strings should not be capitalized (unless beginning with proper nouns or
acronyms) or end with punctuation, as they may be wrapped by other errors and 
appended to a logging message.

```go
// Bad example
func task() error {
    if err := createResource(); err != nil {
        return fmt.Errorf("Unable to create resource foo: %v.", err)
    }
    return nil
}

func createResource() error {
    return fmt.Errorf("Something bad happened.")
}

func Run() {
    if err := task(); err != nil {
        klog.ErrorS(err, "Failed to run task bar")
    }
}

// E0724 00:33:22.895868   24320 main.go:127] "Failed to run task bar" err="Unable to create resource foo: Something bad happened.."

// Good example
func task() error {
    if err := createResource(); err != nil {
        return fmt.Errorf("unable to create resource foo: %v", err)
    }
    return nil
}

func createResource() error {
    return fmt.Errorf("something bad happened")
}

func Run() {
    if err := task(); err != nil {
        klog.ErrorS(err, "Failed to run task bar")
    }
}

// E0724 00:33:22.895868   24320 main.go:127] "Failed to run task bar" err="unable to create resource foo: something bad happened"
```

## Handle the error or return it

```go
// Bad example
func taskA() error {
	if err := createResourceB(); err != nil {
		klog.ErrorS(err, "Unable to create resource B")
		return fmt.Errorf("unable to create resource B: %v", err)
	}
	if err := createResourceC(); err != nil {
		klog.ErrorS(err, "Unable to create resource C")
		return fmt.Errorf("unable to create resource C: %v", err)
	}
	return nil
}

func createResourceB() error {
	return nil
}

func createResourceC() error {
	...
	klog.ErrorS(nil, "Something bad happened")
	return fmt.Errorf("something bad happened")
}

func Run() {
	if err := taskA(); err != nil {
		klog.ErrorS(err, "Failed to run task A")
	}
	return
}
```

It leads to duplicate errors in logs, the formers don't provide more information than the last one.
```text
E0724 00:40:59.485113   24635 main.go:124] "Something bad happened"
E0724 00:40:59.485215   24635 main.go:113] "Unable to create resource C" err="something bad happened"
E0724 00:40:59.485221   24635 main.go:130] "Failed to run task A" err="unable to create resource C: something bad happened"
```

## Utility functions should just return error if there is one

Utility functions should just return error, instead of logging error themselves
or exit program, because the functions may be used in multiple scenarios that
wish to control output or execution flow. An error that is fatal to one caller
may be recoverable or ignorable to another caller.

```go
// Bad example
func GetIPNetDeviceByName(ifaceName string) (v4IPNet *net.IPNet, v6IPNet *net.IPNet, link *net.Interface, err error) {
	link, err = interfaceByName(ifaceName)
	if err != nil {
		klog.ErrorS(err, "Failed to find interface", "name", ifaceName)
		return nil, nil, nil, err
	}
	...
}

func GetIPNetDeviceByName(ifaceName string) (v4IPNet *net.IPNet, v6IPNet *net.IPNet, link *net.Interface, err error) {
	link, err = interfaceByName(ifaceName)
	if err != nil {
		klog.Fatalf("Failed to find interface %s", ifaceName)
	}
	...
}

// Good example
func GetIPNetDeviceByName(ifaceName string) (v4IPNet *net.IPNet, v6IPNet *net.IPNet, link *net.Interface, err error) {
	link, err = interfaceByName(ifaceName)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("interface %s not found", ifaceName)
	}
	...
}
```

## Logging convention

* Log messages should start with a capital letter, and should *not* end with a
period.
* Use past tense to show what happened, e.g. "Created something", and use
present participle to show what the program is going to do, e.g. "Creating
something".
* Use structured logging, i.e. `klog.ErrorS` and `klog.InfoS`.

```text
// Bad example
// klog.InfoS("Update Pod status") // Unclear tense
I0724 00:40:59.485215   24635 main.go:113] "Update Pod status"
// klog.Infof("allocate IP %s for Pod %s", ip, pod) // not capitalized
I0724 00:40:59.485215   24635 main.go:113] allocate IP 192.168.0.10 for Pod default/nginx
// klog.InfoS("Updated IP Pool usage", "IP Pool", pool, "usage", usage) // key contains multiple words
I0724 00:40:59.485215   24635 main.go:113] "Updated IP Pool usage" IP Pool="poolA" usage=10

// Good example
I0724 00:40:59.485215   24635 main.go:113] "Updating Pod status" Pod="default/nginx" status="Running"
I0724 00:40:59.485215   24635 main.go:113] "Updated IP Pool usage" IPPool="poolA" usage=10
```

## Do not make all logs default level

Do not make all logs default level to avoid overwhelming consumers. And
Kubernetes keeps logs of limited size for each Pod. No much useful information
would be preserved if there are many verbose logs.

For development, set the log level properly from the beginning and set `-v` when
running programs to get verbose logs, instead of setting all logs to `V(0)` and
adjusting them later.

Logs that could be in the default level: 
* Key steps in initialization: components initialized, data synced, etc.
* Significant state change: leader/member change of a HA cluster, connection
with OVS established or disconnected, etc.
* Unexpected events
* Information about important requests

## Use same level for similar logs

Use same level for similar logs, for example, if a resource's creation is in
default level, its deletion should be in default level as well, otherwise the
logs may confuse readers that the resource is never deleted.

```go
// Bad example
func createResource(name string) {
    klog.InfoS("Created resource", "name", name)
}

func deleteResource(name string) {
    klog.V(2).InfoS("Deleted resource", "name", name)
}

// I0724 00:40:59.485113   24635 main.go:124] "Created resource" name="foo"
// I0724 00:40:59.485113   24635 main.go:124] "Created resource" name="bar"

// Good example
func createResource(name string) {
    klog.InfoS("Created resource", "name", name)
}

func deleteResource(name string) {
    klog.InfoS("Deleted resource", "name", name)
}

// I0724 00:40:59.485113   24635 main.go:124] "Created resource" name="foo"
// I0724 00:40:59.485113   24635 main.go:124] "Deleted resource" name="foo"
// I0724 00:40:59.485113   24635 main.go:124] "Created resource" name="bar"
// I0724 00:40:59.485113   24635 main.go:124] "Deleted resource" name="bar"
```

## Mutex and RWMutex

RWMutex can be held by an arbitrary number of readers or a single writer, allows
concurrent reading. It saves time when there are multiple readers and reading is
more frequent than writing.

## Shared resource creation

When multiple goroutines could create a shared resource, the code that checks
existence of the shared resource should be in the same critical section as the
one that creates the resource.

```go
// Wrong code
resource, exists := getResource()
if exists {
	return resource
}
mutex.Lock()
defer mutex.UnLock()
return createResource()

// Correct code
mutex.Lock()
defer mutex.UnLock()
resource, exists := getResource()
if exists {
    return resource
}
return createResource()
```

## Implement controller properly

![](https://github.com/kubernetes/sample-controller/blob/master/docs/images/client-go-controller-interaction.jpeg)

Controllers are control loops that watch the state of your cluster, then make or
request changes where needed. Each controller tries to move the current cluster
state closer to the desired state.

A controller tracks at least one Kubernetes resource type. It usually consists
of an informer, a lister, resource event handlers, a workqueue  and workers.

An informer keeps its cache in sync with kube-apiserver and pops objects when
there are changes. 

A lister provides methods to list/get objects from the informer's store.

Resource event handlers are the callback functions which will be called by the
Informer when it wants to deliver an object to your controller. The typical
pattern to write these functions is to obtain the dispatched object’s key and
enqueue that key in a work queue for further processing. Event Handlers are
executed sequentially.

Workqueue decouples delivery of an object from its processing. Before an item
is handled, multiple deliveries of the item lead to only one processing. 
Besides, it guarantees that multiple workers will not end up processing the same
object at the same time.

Worker is the function that you create to process items from the work queue.
There can be multiple workers running in parallel. Workqueue guarantees they
will not process the same object at the same time so they have less race
conditions to consider. A work typically use lister to retrieve the object
corresponding to the key.

https://github.com/kubernetes/sample-controller/blob/master/docs/controller-client-go.md

## Return concrete types

In most cases, constructors should return concrete types instead of interfaces,
which gives consumers the freedom to consume one, multiple or all methods that
are specific to that type and to define interfaces with a subset of methods on
the consumer side.

```go
// Good example
type Subscriber interface {
	Subscribe(h eventHandler)
}

type Notifier interface {
	Notify(interface{}) bool
}

// SubscribableChannel implements both Subscriber and Notifier.
type SubscribableChannel struct { ... }

func NewSubscribableChannel() *SubscribableChannel {
	return &SubscribableChannel{ ... }
}

func NewComponentA(s Subscriber) *ComponentA { ... }

func NewComponentB(n Notifier) *ComponentB { ... }

func main() {
	channel := NewSubscribableChannel()
	// Component A only consumes messages
	componentA := NewComponentA(channel)
	// Component B only produces messages
	componentB := NewComponentB(channel)
}
```

## Use efficient data structure

Use efficient data structure when handling a reasonable number of items. For
instance, to find Pods in Pod list A but not in Pod list B, brute force search
leads to O(N^2) time complexity, while using set could just be O(N). For a scale
of 5,000 Pods, it might be 100ms vs. 2ms. However, do not over-optimize all
cases, for instance, when the slice has only a handful of items, there is no
performance advantage to use set.

```go
// Construct a set based on a slice of string.
set := sets.NewString(items...)
// Construct a set based on keys of a map.
set := sets.StringKeySet(itemMap)
```

## Use Indexer properly

`cache.Indexer` is a generic thread-safe object storage and processing interface
with multiple indices. It takes O(1) time complexity to get items whose non-key
fields match the provided value if the field is indexed.

You must not modify anything returned by `Get` or `List` as it will break the
indexing feature in addition to not being thread safe. For example, a pointer
inserted in the store through `Add` will be returned as is by `Get`. Multiple
clients might invoke `Get` on the same key and modify the pointer in a
non-thread-safe way. Also note that modifying objects stored by the indexers (if
any) will *not* automatically lead to a re-index.

This applies to various K8s `Lister` (e.g. NamespaceLister, PodLister) as they
are built on `Indexer`.

```go
// UID indexing function
func uidIndexFunc(obj interface{}) ([]string, error) {
    meta, err := meta.Accessor(obj)
    if err != nil {
        return []string{""}, fmt.Errorf("object has no meta: %v", err)
    }
    return []string{string(meta.GetUID())}, nil
}
// Status indexing function
func statusIndexFunc(obj interface{}) ([]string, error) {
    return []string{string(obj.(*Pod).GetStatus())}, nil
}
// Construct an indexer.
indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{
	cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
	uidIndex: uidIndexFunc,
    statusIndex: statusIndexFunc,
})

runningPods, _ := indexer.ByIndex(statusIndex, "Running")

for _, pod := range runningPods {
    // Wrong code
    pod.(*Pod).Status = "Success"
	
	// Correct code
	podToUpdate := pod.(*.Pod).DeepCopy()
    podToUpdate.Status = "Success"
    indexer.Update(podToUpdate)
}
```

## Table driven test

Declare a structure to hold the test name, inputs and expected outputs.

Do not make one subtest depend on another, which makes adding test cases hard as
they could affect each other and makes running specific subtests impossible.

```go
// Bad example
func TestDoSomething(t *testing.T) {
	tests := []struct {
		name      string
		inputArg1 string
		inputArg2 string
		want      string
	}{
		{name: "case 1", ...},
		{name: "case 2", ...}, // case 2 depends on case 1 running first 
		{name: "case 3", ...}, // case 3 depends on case 1 and 2 running first
	}

	component := NewComponent()
	for _, tc := range tests {
		got := component.DoSomething(tc.inputArg1, tc.inputArg2)
		assert.Equal(t, tc.want, got)
	}
}

// Good example
func TestDoSomething(t *testing.T) {
    tests := []struct {
        name      string
        inputArg1 string
        inputArg2 string
        want      string
    }{
        {name: "case 1", ...},
        {name: "case 2", ...},
        {name: "case 3", ...},
    }

    for _, tc := range tests {
        t.Run(tc.name, func(t *testing.T) {
            component := NewComponent()
            got := component.DoSomething(tc.inputArg1, tc.inputArg2)
            assert.Equal(t, tc.want, got)
        })
    }
}

```

## Use mocking for unit tests

Unit tests should be reliable and self-contained. Sometimes it's hard to achieve
because of some dependencies. Mocking can make tests easier and controllable.

Use function variable when you need to test a package level function:

```go
func GetIPNetDeviceByName(ifaceName string) (v4IPNet *net.IPNet, v6IPNet *net.IPNet, link *net.Interface, err error) {
	link, err = net.InterfaceByName(ifaceName) // External dependency makes it hard to test directly.
	...
}

// Declare a package level function variable.
var interfaceByName = net.InterfaceByName

func GetIPNetDeviceByName(ifaceName string) (v4IPNet *net.IPNet, v6IPNet *net.IPNet, link *net.Interface, err error) {
    link, err = interfaceByName(ifaceName)  // Use the variable for actual call.
    ...
}

func TestGetIPNetDeviceByName(t *testing.T) {
	tests := []struct {
		name          string
		interfaceName string
		interface     *net.Interface
		wantV4IPNet   *net.IPNet
		wantV6IPNet   *net.IPNet
		wantLink      *net.Interface
		wantErr       error
	}{
		{name: "case 1", ...},
		{name: "case 2", ...},
		{name: "case 3", ...},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Mock the variable for test.
			interfaceByName = func(name string) (*net.Interface, error) {
				return tc.interface 
			}
			defer func() {
				interfaceByName = net.InterfaceByName
			}()
			gotV4IPNet, gotV6IPNet, gotLink, gotErr := GetIPNetDeviceByName(tc.name)
			assert.Equal(t, tc.wantV4IPNet, gotV4IPNet)
			assert.Equal(t, tc.wantV6IPNet, gotV6IPNet)
			assert.Equal(t, tc.wantLink, gotLink)
			assert.Equal(t, tc.wantErr, gotErr)
		})
	}
}
```

Similarly, you could use member variables when you are testing a struct's
methods:

```go
type Client struct {}

func (c *Client) AddSNATRule(snatIP net.IP, mark uint32) error {
	...
	err := iptables.InsertRule(protocol, iptables.NATTable, antreaPostRoutingChain, c.snatRuleSpec(snatIP, mark))
	...
}

type Client struct {
	// insertRule is added as a member to the struct to allow injection for testing.
	insertRule func(protocol Protocol, table string, chain string, ruleSpec []string) error
}

func NewClient() *Client {
	c := &Client{
		insertRule: iptables.InsertRule,
    }
	return c
}

func (c *Client) AddSNATRule(snatIP net.IP, mark uint32) error {
	...
	err := c.insertRule(protocol, iptables.NATTable, antreaPostRoutingChain, c.snatRuleSpec(snatIP, mark))
	...
}

func TestAddSNATRule(t *testing.T) {
	tests := []struct {
		name    string
		snatIP  net.IP
		mark    uint32
		wantErr error
	}{
		{name: "case 1", ...},
		{name: "case 2", ...},
		{name: "case 3", ...},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fakeInsertRule := func(protocol Protocol, table string, chain string, ruleSpec []string) error {
				...
            }
			c := &Client{insertRule: fakeInsertRule}
			gotErr := c.AddSNATRule(tc.snatIP, tc.mark)
			assert.Equal(t, tc.wantErr, gotErr)
		})
	}
}
```

You could also use interface substitution when you are testing a struct's
methods:

```go
type Client struct {}

func (c *Client) AddNodePort(nodePortAddresses []net.IP, port uint16, protocol binding.Protocol) error {
	if err := ipset.CreateIPSet(ipSetName, ipset.HashNet, false); err != nil {
		return err
	}
	...
	if err := ipset.AddEntry(ipSetName, ipSetEntry); err != nil {
		return err
	}
	...
}

type Interface interface {
	CreateIPSet(name string, setType SetType, isIPv6 bool) error

	AddEntry(name string, entry string) error
}

type Client struct {
	// ipset defines an interface for ipset operations.
	// Added as a member to the struct to allow injection for testing.
	ipset ipset.Interface
}

// Target function
func (c *Client) AddNodePort(nodePortAddresses []net.IP, port uint16, protocol binding.Protocol) error {
	if err := c.ipset.CreateIPSet(ipSetName, ipset.HashNet, false); err != nil {
		return err
	}
	...
	if err := c.ipset.AddEntry(ipSetName, ipSetEntry); err != nil {
		return err
	}
	...
}

func TestAddNodePort(t *testing.T) {
	tests := []struct {
		name              string
		nodePortAddresses []net.IP
		port              uint16
		protocol          binding.Protocol
		wantCalls         func(mockIPSet *ipsettest.MockInterface)
		wantErr           error
	}{
		{name: "case 1", ...},
		{name: "case 2", ...},
		{name: "case 3", ...},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			controller := gomock.NewController(t)
			mockIPSet := ipsettest.NewMockInterface(controller)
			c := &Client{ipset: mockIPSet}
			tc.wantCalls(mockIPSet)
			gotErr := c.AddNodePort(tc.nodePortAddresses, tc.port, tc.protocol)
			assert.Equal(t, tc.wantErr, gotErr)
		})
	}
}
```

## Commit Message

* Keep the subject line as short as possible, typically under 50 characters, 
which is not a hard limit but any subject line longer than 72 characters will be
truncated by Github.

* Wrap the body at ~72 characters (not a hard limit, 76 characters and 79
characters are often recommended too) to make them look nice when viewing git
log. This doesn't apply to special cases like long links, and table-style
outputs.

* Use the body to explain what, why and how.

* Link the issue with known Github keywords. For example, Use `Fixes #100`,
`Closes #100` when the commit can resolve the issue completely, then merging the
pull request will close the referenced issue automatically. To link an issue
without closing it, use a different keyword like `For #100`.

A good example is as below:

```text
Improve install_cni_chaining to support updates to CNI conf file

The script is in charge of overwriting the cloud-specific CNI conf file
(e.g., 10-aws.conf for EKS).
However, the script is currently run as an initContainer, and does not
account for the possibility that the CNI conf file may be modified again
by the cloud provider at a later time, hence discarding the changes
made by the script.
For example, restarting aws-node on EKS will cause the 10-aws.conf file
to be overwritten with the default configuration, and Antrea will no
longer be involved in Pod networking configuration. For the user,
everything may appear to work from a connectivity standpoint, but
NetworkPolicies will not be enforced!

To avoid this issue, we run install_cni_chaining in a "normal"
container, and leverage inotify to monitor the CNI conf file. Every time
another process writes to the file, we process it one more time and
update it again if necessary.

This solution is not perfect. I think that there is a small possibility
of race conditions, but they remain very unlikely. One example is this
sequence of events:
1. aws-node overwrites the CNI conf file (because of a restart?)
2. a new Pod is created on the Node, the Antrea CNI is not used
3. install_cni_chaining updates the CNI conf file and adds Antrea to the
   chain

Avoiding this race would require some major changes (e.g., to
antrea-eks-node-init). Because changes to the CNI conf file are *very*
infrequent, I think this is acceptable.

This solution is loosely based on the linkerd CNI installation script:
https://github.com/linkerd/linkerd2/blob/main/cni-plugin/deployment/scripts/install-cni.sh

Fixes #3974

Signed-off-by: Antonin Bas <abas@vmware.com>
```

