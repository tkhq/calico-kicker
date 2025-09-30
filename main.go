package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jsimonetti/rtnetlink/v2"
	"github.com/tkhq/calico-kicker/calico"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	defaultNamespace = "calico"
)

var (
	checkInterval        time.Duration
	terminationTimeout   time.Duration
	calicoInterfaceNames []string

	// DeletionGracePeriodSeconds is the number of seconds to allow for the Pod to be gracefully shut down when we are requesting the destruction of our own Pod.
	DeletionGracePeriodSeconds int64 = 10
)

func init() {
	flag.DurationVar(&checkInterval, "check-interval", 30*time.Second, "interval at which to check for addresses")
	flag.DurationVar(&terminationTimeout, "termination-timeout", 3*time.Minute, "total amount of time to allow for the addresses to come available before killing Pod")
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

	ctx, cancel := context.WithTimeout(context.Background(), terminationTimeout)
	defer cancel()

	slog.Info("starting address monitor")

	for range ticker.C {
		if calico.AllIPsExist(conn, calicoInterfaceNames) {
			slog.Info("all Calico addresses exist")

			break
		}

		if ctx.Err() != nil {
			slog.Error("time to wait for address to come live has expired; killing Pod")

			killPod()
		}
	}

	// sleep forever
	for range time.After(time.Hour) {
		slog.Debug("no-op")
	}
}

func killPod() {
	slog.Debug("terminating our own Pod")

	podNamespace := os.Getenv("POD_NAMESPACE")
	if podNamespace == "" {
		podNamespace = defaultNamespace
	}

	podName := os.Getenv("POD_NAME")
	if podName == "" {
		slog.Error("POD_NAME environment variable is not set; cannot kill own Pod")

		return
	}

	cfg, err := rest.InClusterConfig()
	if err != nil {
		slog.Error("failed to create in-cluster kubernetes client config", slog.String("error", err.Error()))

		return
	}

	clientSet, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		slog.Error("failed to create kubernetes client set", slog.String("error", err.Error()))

		return
	}

	if err := clientSet.CoreV1().Pods(podNamespace).Delete(context.Background(), podName, metav1.DeleteOptions{
		GracePeriodSeconds: &DeletionGracePeriodSeconds,
	}); err != nil {
		slog.Error("failed to kill own Pod", slog.String("error", err.Error()),
			slog.String("pod_name", podName),
			slog.String("pod_namespace", podNamespace),
		)

		return
	}

	slog.Info("scheduled deletion of own Pod",
		slog.String("pod_name", podName),
		slog.String("pod_namespace", podNamespace),
	)
}
