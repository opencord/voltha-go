/*
 * Copyright 2018-present Open Networking Foundation

 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at

 * http://www.apache.org/licenses/LICENSE-2.0

 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/opencord/voltha-go/common/log"
	"github.com/opencord/voltha-go/ofagent/common"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Main struct {
	connection_mgr      *ofagent.ConnectionManager
	defaults            map[string]string
	controllers         []string
	configPath          string
	consulType          string
	grpcEndpoint        string
	coreBindingKey      string
	externalHostAddress string
	internalHostAddress string
	instanceId          string
	workingDir          string
	containerName       string
	keyFile             string
	certFile            string
	grpcTimeout         int
	exiting             bool
	nobanner            bool
	quiet               bool
	verbose             bool
	enableTls           bool
}

func NewMain() *Main {
	var theMain Main
	theMain.connection_mgr = nil
	theMain.defaults = make(map[string]string)
	theMain.grpcTimeout = 0
	theMain.exiting = false
	theMain.nobanner = false
	theMain.quiet = false
	theMain.verbose = false
	theMain.enableTls = false
	return &theMain
}

func print_localAddresses() {

	fmt.Printf("localAddresses()\n")

	ifaces, err := net.Interfaces()
	if err != nil {
		fmt.Print(fmt.Errorf("localAddresses: %+v\n", err.Error()))
		return
	}
	for _, i := range ifaces {
		addrs, err := i.Addrs()
		if err != nil {
			fmt.Print(fmt.Errorf("localAddresses: %+v\n", err.Error()))
			continue
		}
		b, err := json.MarshalIndent(i, "", "  ")
		if err != nil {
			fmt.Println("error:", err)
		}
		// pretty print json
		fmt.Printf("Addr: %s Interface: %s \n", addrs, string(b))
		for _, a := range addrs {
			switch v := a.(type) {
			case *net.IPAddr:
				fmt.Printf("%v : %s (%s)\n", i.Name, v, v.IP.DefaultMask())
			}
		}
	}
	fmt.Printf("localAddresses() done iface count: %d\n", len(ifaces))
}

func (e Main) create_default_dict() {

	e.defaults["config"] = os.Getenv("CONFIG")
	if len(e.defaults["config"]) == 0 {
		e.defaults["config"] = "./ofagent.yml"
	}
	e.defaults["consul"] = os.Getenv("CONSUL")
	if len(e.defaults["consul"]) == 0 {
		e.defaults["consul"] = "localhost:8500"
	}
	e.defaults["controller"] = os.Getenv("CONTROLLER")
	if len(e.defaults["controller"]) == 0 {
		e.defaults["controller"] = "localhost:6653"
	}
	e.defaults["external_host_address"] = os.Getenv("EXTERNAL_HOST_ADDRESS")
	if len(e.defaults["external_host_address"]) == 0 {
		e.defaults["external_host_address"] = "IPADDRESS" //get_my_primary_local_ipv4()
	}
	e.defaults["grpc_endpoint"] = os.Getenv("GRPC_ENDPOINT")
	if len(e.defaults["grpc_endpoint"]) == 0 {
		e.defaults["grpc_endpoint"] = "localhost:50055"
	}
	e.defaults["grpc_timeout"] = os.Getenv("GRPC_TIMEOUT")
	if len(e.defaults["grpc_timeout"]) == 0 {
		e.defaults["grpc_timeout"] = "10"
	}
	e.defaults["core_binding_key"] = os.Getenv("CORE_BINDING_KEY")
	if len(e.defaults["core_binding_key"]) == 0 {
		e.defaults["core_binding_key"] = "voltha_backend_name"
	}
	e.defaults["instance_id"] = os.Getenv("INSTANCE_ID")
	if len(e.defaults["instance_id"]) == 0 {
		checkHost, err := os.Hostname()
		if err != nil {
			fmt.Printf("Error getting hostname\n")
		} else {
			e.defaults["instance_id"] = checkHost
		}
		//instance_id=os.environ.get('INSTANCE_ID', os.environ.get('HOSTNAME', '1')),
	}

	e.defaults["internal_host_address"] = os.Getenv("INTERNAL_HOST_ADDRESS")
	if len(e.defaults["internal_host_address"]) == 0 {
		e.defaults["internal_host_address"] = "IPADDRESS" //get_my_primary_local_ipv4()
	}
	e.defaults["key_file"] = os.Getenv("KEY_FILE")
	if len(e.defaults["key_file"]) == 0 {
		e.defaults["key_file"] = "/ofagent/pki/voltha.key"
	}
	e.defaults["work_dir"] = os.Getenv("WORK_DIR")
	if len(e.defaults["work_dir"]) == 0 {
		e.defaults["work_dir"] = "/tmp/ofagent"
	}
	e.defaults["cert_file"] = os.Getenv("CERT_FILE")
	if len(e.defaults["cert_file"]) == 0 {
		e.defaults["cert_file"] = "/ofagent/pki/voltha.crt"
	}
}

func (e Main) print_banner() {

	var banner = "\n  ___  _____ _                    _\n / _ \\ |  ___/ \\   __ _  ___ _ __ | |_\n | | | | |_ / _ \\ / _` |/ _ \\ '_ \\| __|\n | |_| |  _/ ___ \\ (_| |  __/ | | | |_\n \\___/ |_|/_/   \\_\\__, |\\___|_| |_|\\__|\n\t\t  |___/\n "
	s := strings.Split(banner, "\n")
	for i := 0; i < len(s); i++ {
		fmt.Println(s[i])
	}
	fmt.Println("(to stop: press Ctrl-C)")
}

func (e Main) start() {
	e.start_reactor() // # will not return except Keyboard interrupt
}

func (e Main) startup_components() {
	fmt.Println("Startup Components")

	if e.defaults != nil {
		grpcTimeout, err := strconv.Atoi(e.defaults["grpc_timeout"])
		if err == nil {
			e.grpcTimeout = grpcTimeout
		}
	} else {
		fmt.Println("Defaults is nil ")
	}
	// put a check here if the binding key isn't set in a passed parameter, use the default
	//coreBindingKey := e.defaults["core_binding_key"]
	//	e.coreBindingKey = coreBindingKey

	// replace with command line parameters if provided
	// enable_tls = true for now
	fmt.Println("Default Controller: ", e.defaults["controller"])
	//if (len(e.controllers) == 0) {
	//append(e.controllers, e.defaults["controller"])
	//}

	// create a connection manager instance
	e.connection_mgr = ofagent.NewConnectionManager(e.defaults["consul"], e.defaults["grpc_endpoint"], e.grpcTimeout, e.coreBindingKey, e.controllers, e.defaults["instance_id"], true, e.defaults["key_file"], e.defaults["cert_file"], 0.5, 5, 5)
	//log.Debugln("ConnectionManager.Start()")
	e.connection_mgr.Start()
}

func (e Main) shutdown_components() {
	log.Debugln("shutdown_components")
	e.exiting = true
	if e.connection_mgr != nil {
		//yield self.connection_manager.stop()
		e.connection_mgr.Stop()
	}
}

func (e Main) start_reactor() {
	var gracefulStop = make(chan os.Signal)
	signal.Notify(gracefulStop, syscall.SIGTERM)
	signal.Notify(gracefulStop, syscall.SIGINT)
	go func() {
		sig := <-gracefulStop
		fmt.Printf("caught sig: %+v", sig)
		fmt.Println("Wait for 2 second to finish processing")
		e.shutdown_components()
		time.Sleep(2 * time.Second)
		os.Exit(0)
	}()
}

/*
   if args.instance_id_is_container_name:
       args.instance_id = get_my_containers_name()

   def start_reactor(self):
       reactor.callWhenRunning(
           lambda: self.log.info('twisted-reactor-started'))
       reactor.addSystemEventTrigger('before', 'shutdown',
                                     e.shutdown_components)
       reactor.run()
*/

func (e Main) initialize() {

	// load the defaults
	e.create_default_dict()
	// set up a custom printout of the --help
	e.setupFlags(flag.CommandLine)
	// set up the possible params to parse for
	e = e.load_valid_flags()

	/*
	   self.args = args = parse_args()
	   self.config = load_config(args)

	   verbosity_adjust = (args.verbose or 0) - (args.quiet or 0)
	   self.log = setup_logging(self.config.get('logging', {}),
	                            args.instance_id,
	                            verbosity_adjust=verbosity_adjust)
	*/
	// print out the banner
	if false == e.nobanner {
		e.print_banner()
	}
	e.startup_components()
}

func (e Main) print_usage() {
	nls := "\n\t\t\t"
	fmt.Printf("Usage: ofagent [...]\n")
	fmt.Printf("  %s %s\t\tPath to ofagent.yml config file (default: %s).%sIf relative, it is relative to main.py of ofagent.\n",
		"-c", "--config", e.defaults["config"], nls)
	fmt.Printf("  -C --consul\t\t<hostname>:<port> to consul agent (default: %s)\n", e.defaults["consul"])
	fmt.Printf("  -E --external-host-address\t<hostname> or <ip> at which ofagent is%sreachable from outside the cluster (default: %s)\n", nls, e.defaults["external_host_address"])
	fmt.Printf("  -G --grpc-endpoint\tgRPC end-point to connect to. It can either be a direct%sdefinition in the form of <hostname>:<port>, or it can be%san indirect definition in the form of @<service-name> %swhere <service-name> is the name of the grpc service as%sregistered in consul (example: @voltha-grpc). (default: %s)\n", nls, nls, nls, nls, e.defaults["grpc_endpoint"])
	fmt.Printf("  -O --controller\t<hostname1>:<port1> <hostname2>:<port2> <hostname3>:<port3> ... <hostnamen>:<portn>%sto openflow controller (default: %s)\n", nls, e.defaults["controller"])
	fmt.Printf("  -T --grpc-timeout\tgRPC timeout in seconds (default: %s)\n", e.defaults["grpc_timeout"])
	fmt.Printf("  -B --core-binding-key\tThe name of the meta-key whose value is the rw-core group to which the%s ofagent's gRPC client is bound. (default: %s)\n", nls, e.defaults["core_binding_key"])
	fmt.Printf("  -H --internal-host-address\t<hostname> or <ip> at which ofagent is reachable from%sinside the cluster (default: %s)\n", nls, e.defaults["internal_host_address"])
	fmt.Printf("  -i --instance-id\tUnique string id of this ofagent instance (default: %s)\n", e.defaults["instance_id"])
	fmt.Printf("  -n --no-banner\tOmit startup banner log lines\n")
	fmt.Printf("  -q --quiet\t\tSuppress debug and info logs\n")
	fmt.Printf("  -v --verbose\t\tEnable verbose logging\n")
	fmt.Printf("  -w --work-dir\t\tWork dir to compile and assemble generated files (default=%s)\n", e.defaults["work_dir"])
	fmt.Printf("  --instance-id-is-container-name\tUse docker container name as ofagent instance id (overrides -i/--instance-id option)\n")
	fmt.Printf("  -t --enable-tls\tSpecify this option to enable TLS security between ofagent and onos.\n")
	fmt.Printf("  -k --key-file\t\tKey file to be used for tls security (default=%s)\n", e.defaults["key_file"])
	fmt.Printf("  -r --cert-file\tCertificate file to be used for tls security (default=%s)\n", e.defaults["cert_file"])
	fmt.Printf("  -h --help\t\tPrint this help.\n")
}

func (e Main) load_valid_flags() Main {

	e.configPath = "file path"

	defaultConfig := fmt.Sprintf("Path to ofagent.yml config file (default: %s).  If relative, it is relative to main.py of ofagent.\n", e.defaults["config"])
	// -c --config
	flag.StringVar(&e.configPath, "c", defaultConfig, "Configure (shorthand)")
	flag.StringVar(&e.configPath, "config", defaultConfig, "Configure")

	// -C --consul
	defaultConsul := fmt.Sprintf("\t-C --consul\t <hostname>:<port> to consul agent (default: %s)\n", e.defaults["consul"])
	usageConsul := "Consul Agent"

	flag.StringVar(&e.consulType, "C", defaultConsul, usageConsul+" (shorthand)")
	flag.StringVar(&e.consulType, "consul", defaultConsul, usageConsul)

	externalHostAddr := fmt.Sprintf("<hostname> or <ip> at which ofagent is reachable from outside the cluster (default: %s)\n", e.defaults["external_host_address"])
	usageExternal := "External Host"

	// -E --external-host-address
	flag.StringVar(&e.externalHostAddress, "E", externalHostAddr, usageExternal)
	flag.StringVar(&e.externalHostAddress, "external-host-address", externalHostAddr, usageExternal)

	grpcEndpoint := fmt.Sprintf("gRPC end-point to connect to. It can either be a direct definition in the form of <hostname>:<port>, or it can be an indirect definition in the form of @<service-name> where <service-name> is the name of the grpc service as registered in consul (example: @voltha-grpc). (default: %s)\n", e.defaults["grpc_endpoint"])
	grpcUsage := "gRPC end-point"
	// -G --grpc-endpoint
	flag.StringVar(&e.grpcEndpoint, "G", grpcEndpoint, grpcUsage)
	flag.StringVar(&e.grpcEndpoint, "grpc-endpoint", grpcEndpoint, grpcUsage)

	//flag.StringVar(&e.controllers, "O", controllers, "<hostname1>:<port1> <hostname2>:<port2> <hostname3>:<port3> ... <hostnamen>:<portn> to openflow controller")
	//flag.StringVar(&e.controllers, "controller", controllers, "<hostname1>:<port1> <hostname2>:<port2> <hostname3>:<port3> ... <hostnamen>:<portn> to openflow controller")

	flag.IntVar(&e.grpcTimeout, "T", e.grpcTimeout, "gRPC timeout in seconds")
	flag.IntVar(&e.grpcTimeout, "grpc-timeout", e.grpcTimeout, "gRPC timeout in seconds")

	flag.StringVar(&e.coreBindingKey, "B", e.coreBindingKey, "The name of the meta-key whose value is the rw-core group to which the%s ofagent's gRPC client is bound.")
	flag.StringVar(&e.coreBindingKey, "core-binding-key", e.coreBindingKey, "The name of the meta-key whose value is the rw-core group to which the%s ofagent's gRPC client is bound.")

	flag.StringVar(&e.internalHostAddress, "H", e.internalHostAddress, "<hostname> or <ip> at which ofagent is reachable from%sinside the cluster")
	flag.StringVar(&e.internalHostAddress, "internal-host-address", e.internalHostAddress, "<hostname> or <ip> at which ofagent is reachable from%sinside the cluster")

	flag.StringVar(&e.instanceId, "i", e.instanceId, "Unique string id of this ofagent instance")
	flag.StringVar(&e.instanceId, "instance-id", e.instanceId, "Unique string id of this ofagent instance")

	flag.BoolVar(&e.nobanner, "n", false, "Omit the startup banner log lines")
	flag.BoolVar(&e.nobanner, "no-banner", false, "Omit the startup banner log lines")

	flag.BoolVar(&e.quiet, "q", false, "Suppress debug and info logs")
	flag.BoolVar(&e.quiet, "quiet", false, "Suppress debug and info logs")

	flag.BoolVar(&e.verbose, "v", false, "Enable verbose logging")
	flag.BoolVar(&e.verbose, "verbose", false, "Enable verbose logging")

	flag.StringVar(&e.workingDir, "w", e.workingDir, "Work dir to compile and assemble generated files")
	flag.StringVar(&e.workingDir, "work-dir", e.workingDir, "Work dir to compile and assemble generated files")

	flag.StringVar(&e.containerName, "instance-id-is-container-name", e.containerName, "Use docker container name as ofagent instance id (overrides -i/--instance-id option)")

	flag.BoolVar(&e.enableTls, "t", false, "Specify this option to enable TLS security between ofagent and onos")
	flag.BoolVar(&e.enableTls, "enable-tls", false, "Specify this option to enable TLS security between ofagent and onos")

	flag.StringVar(&e.keyFile, "k", e.keyFile, "Key file to be used for tls security")
	flag.StringVar(&e.keyFile, "key-file", e.keyFile, "Key file to be used for tls security")

	flag.StringVar(&e.certFile, "r", e.certFile, "Certificate file to be used for tls security")
	flag.StringVar(&e.certFile, "cert-file", e.certFile, "Certificate file to be used for tls security")

	visitor := func(a *flag.Flag) {
		if a.Name == "c" || a.Name == "-config" {
			if _, err := os.Stat(e.configPath); err == nil {
				fmt.Println("Using Config File: ", e.configPath)
			} else if os.IsNotExist(err) {
				e.progArgErrors(fmt.Sprintf("Path doesn't exist", err))
			} else {
				e.progArgErrors(fmt.Sprintf("File exists issue", err))
			}
		}
		if a.Name == "C" || a.Name == "-consul" {
			fmt.Println("- Using Consul: ", e.consulType)
		}
		if a.Name == "E" || a.Name == "-external-host-address" {
			fmt.Println("- Using External Host Address: ", e.externalHostAddress)
		}
		if a.Name == "G" || a.Name == "-grpc-endpoint" {
			fmt.Println("- Using GRPC Endpoint: ", e.grpcEndpoint)
		}
		if a.Name == "O" || a.Name == "-controller" {
			// print array of controllers
			//fmt.Println("- Using Controller: ", e.controller)
		}
		if a.Name == "T" || a.Name == "-grpc-timeout" {
			fmt.Println("- Using GRPC Timeout: ", e.grpcTimeout)
		}
		if a.Name == "B" || a.Name == "-core-binding-key" {
			fmt.Println("- Using Core Binding Key: ", e.coreBindingKey)
		}
		if a.Name == "H" || a.Name == "-internal-host-address" {
			fmt.Println("- Using Internal Host Address: ", e.internalHostAddress)
		}
		if a.Name == "i" || a.Name == "-instance-id" {
			fmt.Println("- Using Instance Id: ", e.instanceId)
		}
		if a.Name == "n" || a.Name == "-no-banner" {
			fmt.Println("- No Banner: ", e.nobanner)
		}
		if a.Name == "q" || a.Name == "-quiet" {
			fmt.Println("- Quiet: ", e.quiet)
		}
		if a.Name == "v" || a.Name == "-verbose" {
			fmt.Println("- Verbose: ", e.verbose)
		}
		if a.Name == "w" || a.Name == "-work-dir" {
			fmt.Println("- Working Directory: ", e.workingDir)
		}
		if a.Name == "-instance-id-is-container-name" {
			fmt.Println("- Instance Id Is Container Name: ", e.containerName)
		}
		if a.Name == "t" || a.Name == "-enable-tls" {
			fmt.Println("- TLS Enabled: ", e.enableTls)
		}
		if a.Name == "k" || a.Name == "-key-file" {
			if _, err := os.Stat(e.keyFile); err == nil {
				fmt.Println("Using KeyFile: ", e.keyFile)
			} else if os.IsNotExist(err) {
				e.progArgErrors(fmt.Sprintf("Path doesn't exist", err))
			} else {
				e.progArgErrors(fmt.Sprintf("File exists issue", err))
			}
		}
		if a.Name == "r" || a.Name == "-cert-file" {
			if _, err := os.Stat(e.certFile); err == nil {
				fmt.Println("Using CertFile: ", e.certFile)
			} else if os.IsNotExist(err) {
				e.progArgErrors(fmt.Sprintf("Path doesn't exist", err))
			} else {
				e.progArgErrors(fmt.Sprintf("File exists issue", err))
			}
		}
	}

	flag.Parse()

	flag.Visit(visitor)
	return e
}

func (e Main) progArgErrors(description string) {
	fmt.Println("Invalid Argument Errors: ", description)
	os.Exit(-1)
}

func load_config() {
	//var path = os.Args
}

/*
def load_config(args):
    path = args.config
    if path.startswith('.'):
        dir = os.path.dirname(os.path.abspath(__file__))
        path = os.path.join(dir, path)
    path = os.path.abspath(path)
    with open(path) as fd:
        config = yaml.load(fd)
    return config
*/

func (e Main) setupFlags(f *flag.FlagSet) {
	// show custom message
	f.Usage = func() {
		e.print_usage()
	}
}

func main() {

	theMain := *NewMain()
	theMain.initialize()

	fmt.Println("Initialized, Starting now")
	theMain.start()

	//fmt.Println(len(os.Args), os.Args)
	//print_localAddresses()
}
