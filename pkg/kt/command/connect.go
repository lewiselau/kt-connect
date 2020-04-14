package command

import (
	"fmt"
	"os"
	"strings"

	"github.com/alibaba/kt-connect/pkg/kt"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	urfave "github.com/urfave/cli"

	"github.com/alibaba/kt-connect/pkg/kt/options"
	"github.com/alibaba/kt-connect/pkg/kt/util"
)

// newConnectCommand return new connect command
func newConnectCommand(cli kt.CliInterface, options *options.DaemonOptions, action ActionInterface) urfave.Command {
	return urfave.Command{
		Name:  "connect",
		Usage: "connection to kubernetes cluster",
		Flags: ConnectActionFlag(options),
		Action: func(c *urfave.Context) error {
			if options.Debug {
				zerolog.SetGlobalLevel(zerolog.DebugLevel)
			}
			return action.Connect(cli, options)
		},
	}
}

// Connect connect vpn to kubernetes cluster
func (action *Action) Connect(cli kt.CliInterface, options *options.DaemonOptions) (err error) {
	if util.IsDaemonRunning(options.RuntimeOptions.PidFile) {
		return fmt.Errorf("connect already running %s exit this", options.RuntimeOptions.PidFile)
	}
	ch := SetUpCloseHandler(cli, options, "connect")
	if err = connectToCluster(cli, options); err != nil {
		return
	}
	// watch background process, clean the workspace and exit if background process occur exception
	go func() {
		<-util.Interrupt()
		CleanupWorkspace(cli, options)
		os.Exit(0)
	}()
	s := <-ch
	log.Info().Msgf("Terminal Signal is %s", s)
	return
}

func connectToCluster(cli kt.CliInterface, options *options.DaemonOptions) (err error) {

	pid, err := util.WritePidFile(options.RuntimeOptions.PidFile)
	if err != nil {
		return
	}
	log.Info().Msgf("Connect Start At %d", pid)

	shadow := cli.Shadow()
	kubernetes, err := cli.Kubernetes()

	if err != nil {
		return
	}

	if options.ConnectOptions.Dump2Hosts {
		hosts := kubernetes.ServiceHosts(options.Namespace)
		for k, v := range hosts {
			log.Debug().Msgf("Service found: %s %s", k, v)
		}
		if options.ConnectOptions.Dump2HostsNamespaces != nil {
			for _, namespace := range options.ConnectOptions.Dump2HostsNamespaces {
				if namespace == options.Namespace {
					continue
				}
				log.Debug().Msgf("Serach service in %s namespace...", namespace)
				singleHosts := kubernetes.ServiceHosts(namespace)
				for k, v := range singleHosts {
					if v == "" || v == "None" {
						continue
					}
					log.Debug().Msgf("Service found: %s.%s %s", k, namespace, v)
					hosts[k+"."+namespace] = v
				}
			}
		}
		util.DumpHosts(hosts)
		options.ConnectOptions.Hosts = hosts
	}

	workload := fmt.Sprintf("kt-connect-daemon-%s", strings.ToLower(util.RandomString(5)))
	if options.ConnectOptions.ShareShadow {
		workload = fmt.Sprintf("kt-connect-daemon-connect-shared")
	}
	endPointIP, podName, sshcm, credential, err :=
		kubernetes.GetOrCreateShadow(workload, options.Namespace, options.Image, labels(workload, options), options.Debug, options.ConnectOptions.ShareShadow)

	if err != nil {
		return
	}

	// record shadow name will clean up terminal
	options.RuntimeOptions.Shadow = workload
	options.RuntimeOptions.SSHCM = sshcm

	cidrs, err := kubernetes.ClusterCrids(options.ConnectOptions.CIDR)
	if err != nil {
		return
	}

	return shadow.Outbound(podName, endPointIP, credential, cidrs, cli.Exec())
}

func labels(workload string, options *options.DaemonOptions) map[string]string {
	labels := map[string]string{
		"kt-component": "connect",
		"control-by":   "kt",
	}
	for k, v := range util.String2Map(options.Labels) {
		labels[k] = v
	}
	splits := strings.Split(workload, "-")
	labels["version"] = splits[len(splits)-1]
	return labels
}
