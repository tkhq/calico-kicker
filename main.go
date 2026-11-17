package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jsimonetti/rtnetlink/v2"
	"github.com/tkhq/calico-kicker/calico"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/httpstream"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

const (
	defaultNamespace = "calico"
)

var (
	checkInterval        time.Duration
	terminationTimeout   time.Duration
	remediateAfter       time.Duration
	remediateCommand     string
	calicoContainer      string
	calicoInterfaceNames []string

	// execTimeout bounds each remediation exec attempt.
	execTimeout = 60 * time.Second

	// DeletionGracePeriodSeconds is the number of seconds to allow for the Pod to be gracefully shut down when we are requesting the destruction of our own Pod.
	DeletionGracePeriodSeconds int64 = 10
)

func init() {
	flag.DurationVar(&checkInterval, "check-interval", 30*time.Second, "interval at which to check for addresses")
	flag.DurationVar(&terminationTimeout, "termination-timeout", 3*time.Minute, "total amount of time to allow for the addresses to come available before killing Pod")
	flag.DurationVar(&remediateAfter, "remediate-after", time.Minute, "time without all addresses after which the remediation command is run in the calico-node container on each check")
	flag.StringVar(&remediateCommand, "remediate-command", "sv restart /etc/service/enabled/node-services", "command (split on whitespace) run in the calico-node container to restart the Calico node-services daemon, which hosts the tunnel address allocator")
	flag.StringVar(&calicoContainer, "calico-container", "calico-node", "name of the container in our own Pod in which to run the remediation command")
	flag.Func("interface-names", "list of interface names to be monitored, overriding the defaults", func(input string) error {
		calicoInterfaceNames = strings.Split(input, ",")

		return nil
	})
}

func main() {
	flag.Parse()

	if debug, _ := strconv.ParseBool(os.Getenv("DEBUG")); debug {
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if remediateAfter >= terminationTimeout {
		slog.Error("-remediate-after must be less than -termination-timeout",
			slog.Duration("remediate_after", remediateAfter),
			slog.Duration("termination_timeout", terminationTimeout),
		)

		os.Exit(1)
	}

	if len(strings.Fields(remediateCommand)) == 0 {
		slog.Error("-remediate-command must not be empty")

		os.Exit(1)
	}

	pod, err := newPodClient()
	if err != nil {
		slog.Error("failed to create kubernetes client", slog.String("error", err.Error()))

		os.Exit(1)
	}

	waitAddresses(ctx, pod)

	// sleep until termination signal received
	<-ctx.Done()
}

// podClient acts on our own Pod.
type podClient struct {
	cfg       *rest.Config
	clientSet kubernetes.Interface
	namespace string
	name      string
}

func newPodClient() (*podClient, error) {
	namespace := os.Getenv("POD_NAMESPACE")
	if namespace == "" {
		namespace = defaultNamespace
	}

	name := os.Getenv("POD_NAME")
	if name == "" {
		return nil, errors.New("POD_NAME environment variable is not set")
	}

	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("in-cluster config: %w", err)
	}

	clientSet, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("client set: %w", err)
	}

	return &podClient{cfg: cfg, clientSet: clientSet, namespace: namespace, name: name}, nil
}

// kill requests deletion of our own Pod, reporting whether it was accepted.
func (p *podClient) kill() bool {
	slog.Debug("terminating our own Pod")

	if err := p.clientSet.CoreV1().Pods(p.namespace).Delete(context.Background(), p.name, metav1.DeleteOptions{
		GracePeriodSeconds: &DeletionGracePeriodSeconds,
	}); err != nil {
		slog.Error("failed to kill own Pod", slog.String("error", err.Error()),
			slog.String("pod_name", p.name),
			slog.String("pod_namespace", p.namespace),
		)

		return false
	}

	slog.Info("scheduled deletion of own Pod",
		slog.String("pod_name", p.name),
		slog.String("pod_namespace", p.namespace),
	)

	return true
}

// remediate restarts the daemon that hosts Calico's tunnel address allocator, by running
// remediateCommand in the calico-node container of our own Pod. A fresh daemon allocates missing
// addresses in its initial reconcile, and there is never a second concurrent allocator.
func (p *podClient) remediate(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, execTimeout)
	defer cancel()

	req := p.clientSet.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(p.namespace).
		Name(p.name).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: calicoContainer,
			Command:   strings.Fields(remediateCommand),
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)

	wsExec, err := remotecommand.NewWebSocketExecutor(p.cfg, "GET", req.URL().String())
	if err != nil {
		return fmt.Errorf("websocket executor: %w", err)
	}

	spdyExec, err := remotecommand.NewSPDYExecutor(p.cfg, "POST", req.URL())
	if err != nil {
		return fmt.Errorf("spdy executor: %w", err)
	}

	exec, err := remotecommand.NewFallbackExecutor(wsExec, spdyExec, func(err error) bool { return httpstream.IsUpgradeFailure(err) || httpstream.IsHTTPSProxyError(err) })
	if err != nil {
		return fmt.Errorf("fallback executor: %w", err)
	}

	var stdout, stderr bytes.Buffer

	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{Stdout: &stdout, Stderr: &stderr})

	slog.Info("remediation command output",
		slog.String("stdout", stdout.String()),
		slog.String("stderr", stderr.String()),
	)

	if err != nil {
		return err // includes exec.CodeExitError for a non-zero exit
	}

	// runit's sv prints "timeout:" or "fail:" when the service did not restart.
	if out := stdout.String() + stderr.String(); strings.Contains(out, "timeout:") || strings.Contains(out, "fail:") {
		return errors.New("service restart reported failure: " + strings.TrimSpace(out))
	}

	return nil
}

func waitAddresses(ctx context.Context, pod *podClient) {
	conn, err := rtnetlink.Dial(nil)
	if err != nil {
		slog.Error("failed to create netlink connection", slog.String("error", err.Error()))

		os.Exit(1)
	}

	defer conn.Close()

	if len(calicoInterfaceNames) < 1 || calicoInterfaceNames[0] == "" {
		calicoInterfaceNames = calico.DefaultInterfaceNames
	}

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	monitor(ctx, ticker.C, time.Now,
		func() bool { return calico.AllIPsExist(conn, calicoInterfaceNames) },
		pod.remediate,
		pod.kill,
	)
}

// monitor checks for the addresses on every tick. Once remediateAfter has elapsed without them, it runs
// remediate on each tick; once terminationTimeout has elapsed, it calls kill on each tick until kill
// succeeds. It returns when the addresses exist, kill succeeds, or ctx is done.
func monitor(ctx context.Context, ticks <-chan time.Time, now func() time.Time,
	check func() bool, remediate func(context.Context) error, kill func() bool,
) {
	start := now()
	attempts := 0

	slog.Info("starting address monitor")

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
		}

		if check() {
			slog.Info("all Calico addresses exist")

			return
		}

		elapsed := now().Sub(start)

		if elapsed >= terminationTimeout {
			slog.Error("time to wait for address to come live has expired; killing Pod", slog.Int("remediation_attempts", attempts))

			if kill() {
				return
			}

			continue
		}

		if elapsed < remediateAfter {
			continue
		}

		attempts++

		slog.Info("addresses missing; restarting node-services", slog.Int("attempt", attempts))

		if err := remediate(ctx); err != nil {
			slog.Error("remediation failed", slog.Int("attempt", attempts), slog.String("error", err.Error()))
		} else {
			slog.Info("remediation completed", slog.Int("attempt", attempts))
		}

		if check() {
			slog.Info("all Calico addresses exist after remediation", slog.Int("attempt", attempts))

			return
		}
	}
}
