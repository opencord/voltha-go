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
// gRPC affinity router with active/active backends

package main

// Command line parameters and parsing
import (
	"os"
	"fmt"
	"flag"
	"path"
	//"bufio"
	"errors"
	"os/exec"
	"strconv"
	"io/ioutil"
	"encoding/json"
	"text/template"
	"github.com/golang/protobuf/proto"
	"github.com/opencord/voltha-go/common/log"
	pb "github.com/golang/protobuf/protoc-gen-go/descriptor"
)

type TestConfig struct {
	configFile *string
	logLevel *int
	grpcLog *bool
	Suites []string `json:"suites"`
}


type Connection struct {
	Name string `json:"name"`
	Port string `json:"port"`
}

type ProtoFile struct {
	ImportPath string `json:"importPath"`
	Service string `json:"service"`
	Package string `json:"package"`
}

type ProtoSubst struct {
	From string `json:"from"`
	To string `json:"to"`
}

type Environment struct {
	Command string `json:"cmdLine"`
	ProtoFiles []ProtoFile `json:"protoFiles"`
	ProtoDesc string `json:"protoDesc"`
	Clients []Connection `json:"clients"`
	Servers []Connection `json:"servers"`
	Imports []string `json:"imports"`
	ProtoSubsts []ProtoSubst `json:"protoSubst"`
}

type Rpc struct {
	Client string `json:"client"`
	Method string `json:"method"`
	Param string `json:"param"`
	Expect string `json:"expect"`
	MetaData []MetaD `json:"meta"`
	ExpectMeta []MetaD `json:"expectMeta"`
}

type MetaD struct {
	Key string `json:"key"`
	Val string `json:"value"`
}

type Server struct {
	Name string `json:"name"`
	Meta []MetaD `json:"meta"`
}

type Test struct {
	Name string `json:"name"`
	Send Rpc `json:"send"`
	Servers []Server `json:"servers"`
}

type TestSuite struct {
	Env Environment `json:"environment"`
	Tests []Test `json:"tests"`
}

type ProtoImport struct {
	Service string
	Short string
	Package string
}

type SendItem struct {
	Client string
	Method string
	Param string
	ParamType string
	Expect string
	ExpectType string
	MetaData []MetaD
	ExpectMeta []MetaD
}

type TestCase struct {
	Name string
	Send SendItem
	Srvr []Server
}

type TestList struct {
	ProtoImports []ProtoImport
	Imports []string
	Tests []TestCase
}

type ClientConfig struct {
	Ct int
	Name string
	Port string
	Imports []string
	Methods map[string]*mthd
	ProtoImports []ProtoImport
}

type ServerConfig struct {
	Ct int
	Name string
	Port string
	Imports []string
	Methods map[string]*mthd
	ProtoImports []ProtoImport
}

type mthd struct {
	Pkg string
	Svc string
	Name string
	Param string
	Rtrn string
	Ss bool // Server streaming 
	Cs bool // Clieent streaming 
}


func parseCmd() (*TestConfig, error) {
	config := &TestConfig{}
	cmdParse := flag.NewFlagSet(path.Base(os.Args[0]), flag.ContinueOnError);
	config.configFile = cmdParse.String("config", "suites.json", "The configuration file for the affinity router tester")
	config.logLevel = cmdParse.Int("logLevel", 0, "The log level for the affinity router tester")
	config.grpcLog = cmdParse.Bool("grpclog", false, "Enable GRPC logging")

	err := cmdParse.Parse(os.Args[1:]);
	if err != nil {
		//return err
		return nil, errors.New("Error parsing the command line");
	}
	return config, nil
}

func (conf * TestConfig) loadConfig() error {

	configF, err := os.Open(*conf.configFile);
	log.Info("Loading configuration from: ", *conf.configFile)
	if err != nil {
		log.Error(err)
		return err
	}

	defer configF.Close()

	if configBytes, err := ioutil.ReadAll(configF); err != nil {
		log.Error(err)
		return err
	} else if err := json.Unmarshal(configBytes, conf); err != nil {
		log.Error(err)
		return err
	}

	return nil
}

func (suite * TestSuite) loadSuite(fileN string)  error {
	suiteF, err := os.Open(fileN);
	log.Info("Loading test suite from: ", fileN)
	if err != nil {
		log.Error(err)
		return err
	}

	defer suiteF.Close()

	if suiteBytes, err := ioutil.ReadAll(suiteF); err != nil {
		log.Error(err)
		return err
	} else if err := json.Unmarshal(suiteBytes, suite); err != nil {
		log.Error(err)
		return err
	}

	return nil
}

func loadProtoMap(fileName string, pkg string, svc string, substs []ProtoSubst) (map[string]*mthd, error) {
	var mthds map[string]*mthd = make(map[string]*mthd)
	var rtrn_err bool

	// Load the protobuf descriptor file
	protoDescriptor := &pb.FileDescriptorSet{}
	fb, err := ioutil.ReadFile(fileName);
	if err != nil {
		log.Errorf("Could not open proto file '%s'",fileName)
		rtrn_err = true
	}
	err = proto.Unmarshal(fb, protoDescriptor)
	if err != nil {
		log.Errorf("Could not unmarshal %s, %v", "proto.pb", err)
		rtrn_err = true
	}

	var substM  map[string]string = make(map[string]string)
	// Create a substitution map
	log.Debugf("Creating import map")
	for _,v := range substs {
		log.Debugf("Mapping from %s to %s", v.From, v.To)
		substM[v.From] = v.To
	}


	// Build the a map containing the method as the key
	// and the paramter and return types as the fields
	for _,f := range protoDescriptor.File {
		if *f.Package == pkg {
			for _, s:= range f.Service {
				if *s.Name == svc {
					log.Debugf("Loading package data '%s' for service '%s'", *f.Package, *s.Name)
					// Now create a map keyed by method name with the value being the
					// field number of the route selector.
					//var ok bool
					for _,m := range s.Method {
						// Find the input type in the messages and extract the
						// field number and save it for future reference.
						log.Debugf("Processing method (%s(%s) (%s){}",*m.Name, (*m.InputType)[1:], (*m.OutputType)[1:])
						mthds[*m.Name] = &mthd{Pkg:pkg, Svc:svc, Name:*m.Name, Param:(*m.InputType)[1:],
												Rtrn:(*m.OutputType)[1:]}
						if m.ClientStreaming != nil && *m.ClientStreaming == true {
							log.Debugf("Method %s is a client streaming method", *m.Name)
							mthds[*m.Name].Cs = true
						}
						if m.ServerStreaming != nil && *m.ServerStreaming == true {
							log.Debugf("Method %s is a server streaming method", *m.Name)
							mthds[*m.Name].Ss = true
						}
						// Perform the required substitutions
						if _,ok := substM[mthds[*m.Name].Param]; ok == true {
							mthds[*m.Name].Param = substM[mthds[*m.Name].Param]
						}
						if _,ok := substM[mthds[*m.Name].Rtrn]; ok == true {
							mthds[*m.Name].Rtrn = substM[mthds[*m.Name].Rtrn]
						}
					}
				}
			}
		}
	}
	if rtrn_err {
		return nil,errors.New(fmt.Sprintf("Failed to load protobuf descriptor file '%s'",fileName))
	}
	return mthds, nil
}

// Server source code generation
func generateServers(conf *TestConfig, suiteDir string, ts * TestSuite,
						t *template.Template) error {
	var servers []ServerConfig

	for k,v := range ts.Env.Servers {
		log.Infof("Generating the code for server[%d]: %s", k, v.Name)
		sc := &ServerConfig{Name:v.Name, Port:v.Port, Ct:k, Imports:ts.Env.Imports}
		for k1,v1 := range ts.Env.ProtoFiles {
			imp := &ProtoImport{Short:"pb"+strconv.Itoa(k1),
								Package:v1.ImportPath+v1.Package,
								Service:v1.Service}
			imp = &ProtoImport{Short:v1.Package,
								Package:v1.ImportPath+v1.Package,
								Service:v1.Service}
			sc.ProtoImports = append(sc.ProtoImports, *imp)
			// Compile the template from the file
			log.Debugf("Proto substs: %v", ts.Env.ProtoSubsts)
			if mthds, err := loadProtoMap(ts.Env.ProtoDesc, v1.Package,
											v1.Service, ts.Env.ProtoSubsts); err != nil {
				log.Errorf("Unable to process proto descriptor file %s for package: %s, service: %s",
						ts.Env.ProtoDesc, v1.Package, v1.Service)
				return err
			} else {
				//Generate all the function calls required by the 
				sc.Methods = mthds
			}
		}
		log.Debugf("Server: %v", *sc)
		// Save this server for the next steop
		servers = append(servers, *sc)
		// Open an output file to put the output in.
		if f,err := os.Create(suiteDir+"/"+v.Name+".go"); err == nil {
			defer f.Close()
			//if err := t.ExecuteTemplate(os.Stdout, "server.go", *sc); err != nil {}
			if err := t.ExecuteTemplate(f, "server.go", *sc); err != nil {
				log.Errorf("Unable to execute template for server[%d]: %s: %v", k, v.Name, err)
				return err
			}
		}
	}
	// Generate the server initialization code
	if f,err := os.Create(suiteDir+"/serverInit.go"); err == nil {
		defer f.Close()
		//if err := t.ExecuteTemplate(os.Stdout, "server.go", *sc); err != nil {}
		if err := t.ExecuteTemplate(f, "serverInit.go", servers); err != nil {
			log.Errorf("Unable to execute template for serverInit.go: %v", err)
			return err
		}
	}

	return nil
}

func generateClients(conf *TestConfig, suiteDir string, ts * TestSuite,
						t *template.Template) error {
	var clients []ClientConfig
	for k,v := range ts.Env.Clients {
		log.Infof("Generating the code for client[%d]: %s", k, v.Name)
		cc := &ClientConfig{Name:v.Name, Port:v.Port, Ct:k, Imports:ts.Env.Imports}
		// TODO: This loop makes no sense, the only proto map that would remain
		// after this loop is the last one loaded. Fix this to load the map
		// for all services not just the last one.
		for _,v1 := range ts.Env.ProtoFiles {
			imp := &ProtoImport{Short:v1.Package,
								Package:v1.ImportPath+v1.Package,
								Service:v1.Service}
			cc.ProtoImports = append(cc.ProtoImports, *imp)
			// Compile the template from the file
			log.Debugf("Proto substs: %v", ts.Env.ProtoSubsts)
			if mthds, err := loadProtoMap(ts.Env.ProtoDesc, v1.Package,
											v1.Service, ts.Env.ProtoSubsts); err != nil {
				log.Errorf("Unable to process proto descriptor file %s for package: %s, service: %s",
						ts.Env.ProtoDesc, v1.Package, v1.Service)
				return err
			} else {
				//Generate all the function calls required by the 
				cc.Methods = mthds
			}
		}
		clients = append(clients, *cc)
		if f,err := os.Create(suiteDir+"/"+v.Name+".go"); err == nil {
			_=f
			defer f.Close()
			if err := t.ExecuteTemplate(f, "client.go", cc); err != nil {
				log.Errorf("Unable to execute template for client.go: %v", err)
				return err
			}
		} else {
			log.Errorf("Couldn't create file %s : %v", suiteDir+"/client.go", err)
			return err
		}
	}
	if f,err := os.Create(suiteDir+"/clientInit.go"); err == nil {
		defer f.Close()
		//if err := t.ExecuteTemplate(os.Stdout, "server.go", *sc); err != nil {}
		if err := t.ExecuteTemplate(f, "clientInit.go", clients); err != nil {
			log.Errorf("Unable to execute template for clientInit.go: %v", err)
			return err
		}
	}
	return nil
}

func serverExists(srvr string, ts * TestSuite) bool {
	for _,v := range ts.Env.Servers {
		if v.Name == srvr {
			return true
		}
	}
	return false
}
func generateTestCases(conf *TestConfig, suiteDir string, ts * TestSuite,
						t *template.Template) error {
	var mthdMap map[string]*mthd
	// Generate the test cases
	log.Info("Generating the test cases: runTests.go")
	tc := &TestList{Imports:ts.Env.Imports}

	// Load the proto descriptor file
	// TODO: This loop makes no sense, the only proto map that would remain
	// after this loop is the last one loaded. Fix this to load the map
	// for all services not just the last one.
	for _,v := range ts.Env.ProtoFiles {
		imp := &ProtoImport{Short:v.Package,
							Package:v.ImportPath+v.Package,
							Service:v.Service}
		tc.ProtoImports = append(tc.ProtoImports, *imp)
		// Compile the template from the file
		log.Debugf("Proto substs: %v", ts.Env.ProtoSubsts)
		if mthds, err := loadProtoMap(ts.Env.ProtoDesc, v.Package,
										v.Service, ts.Env.ProtoSubsts); err != nil {
			log.Errorf("Unable to process proto descriptor file %s for package: %s, service: %s",
					ts.Env.ProtoDesc, v.Package, v.Service)
			return err
		} else {
			mthdMap = mthds
		}
	}
	// Create the test data structure for the template
	for _,v := range ts.Tests {
		var test TestCase

		test.Name = v.Name
		test.Send.Client = v.Send.Client
		test.Send.Method = v.Send.Method
		test.Send.Param = v.Send.Param
		test.Send.ParamType = mthdMap[test.Send.Method].Param
		test.Send.Expect = v.Send.Expect
		test.Send.ExpectType = mthdMap[test.Send.Method].Rtrn
		for _,v1 := range v.Send.MetaData {
			test.Send.MetaData = append(test.Send.MetaData,v1)
		}
		for _,v1 := range v.Send.ExpectMeta {
			test.Send.ExpectMeta = append(test.Send.ExpectMeta,v1)
		}
		for _,v1 := range v.Servers {
			var srvr Server
			if serverExists(v1.Name, ts) == false {
				log.Errorf("Server '%s' is not defined!!", v1.Name)
				return errors.New(fmt.Sprintf("Failed to build test case %s", v.Name))
			}
			srvr.Name = v1.Name
			srvr.Meta = v1.Meta
			test.Srvr = append(test.Srvr, srvr)
		}
		tc.Tests = append(tc.Tests, test)
	}
	if f,err := os.Create(suiteDir+"/runTests.go"); err == nil {
		if err := t.ExecuteTemplate(f, "runTests.go", tc); err != nil {
				log.Errorf("Unable to execute template for runTests.go: %v", err)
		}
		f.Close()
	} else {
		log.Errorf("Couldn't create file %s : %v", suiteDir+"/runTests.go", err)
	}
	return nil
}

func generateTestSuites(conf *TestConfig, srcDir string, outDir string) error {

	// Create a directory for the tests
	if err := os.Mkdir(srcDir, 0777); err != nil {
		log.Errorf("Unable to create directory 'tests':%v\n", err)
		return err
	}

	for k,v := range conf.Suites {
	    var suiteDir string = srcDir+"/"+v
		log.Debugf("Suite[%d] - %s", k, v)
		ts := &TestSuite{}
		ts.loadSuite(v)
		log.Debugf("Suite %s: %v", v, ts)
		log.Info("Processing test suite %s", v)

		t := template.Must(template.New("").ParseFiles("../templates/server.go",
														"../templates/serverInit.go",
														"../templates/client.go",
														"../templates/clientInit.go",
														"../templates/runTests.go",
														"../templates/main.go"))
		// Create a directory for he source code for this test suite
		if err := os.Mkdir(suiteDir, 0777); err != nil {
			log.Errorf("Unable to create directory '%s':%v\n", v, err)
			return err
		}
		// Generate the server source files
		if err := generateServers(conf, suiteDir, ts, t); err != nil {
			log.Errorf("Unable to generate server source files: %v", err)
			return err
		}
		// Generate the client source files
		if err := generateClients(conf, suiteDir, ts, t); err != nil {
			log.Errorf("Unable to generate client source files: %v", err)
			return err
		}
		// Generate the test case source file
		if err := generateTestCases(conf, suiteDir, ts, t); err != nil {
			log.Errorf("Unable to generate test case source file: %v", err)
			return err
		}

		// Finally generate the main file
		log.Info("Generating main.go")
		if f,err := os.Create(suiteDir+"/main.go"); err == nil {
			if err := t.ExecuteTemplate(f, "main.go", ts.Env); err != nil {
					log.Errorf("Unable to execute template for main.go: %v", err)
			}
			f.Close()
		} else {
			log.Errorf("Couldn't create file %s : %v", suiteDir+"/main.go", err)
		}

		log.Infof("Compiling test suite: %s in directory %s", v, suiteDir)
		if err := os.Chdir(suiteDir); err != nil {
			log.Errorf("Could not change to directory '%s':%v",suiteDir, err)
		}
		cmd := exec.Command("go", "build", "-o", outDir+"/"+v[:len(v)-5])
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			log.Errorf("Error running the compile command:%v", err)
		}
		if err := os.Chdir("../../suites"); err != nil {
			log.Errorf("Could not change to directory '%s':%v","../../suites", err)
		}

		// Now generate the test cases

	}
	return nil

}

func generateTestDriver(conf *TestConfig, srcDir string, outDir string) error {
	// Generate the main test driver file
	if err := os.Mkdir(srcDir, 0777); err != nil {
		log.Errorf("Unable to create directory 'driver':%v\n", err)
		return err
	}
	t := template.Must(template.New("").ParseFiles("../templates/runAll.go"))
	if f,err := os.Create(srcDir+"/runAll.go"); err == nil {
		if err := t.ExecuteTemplate(f, "runAll.go", conf.Suites); err != nil {
				log.Errorf("Unable to execute template for main.go: %v", err)
		}
		f.Close()
	} else {
		log.Errorf("Couldn't create file %s : %v", srcDir+"/runAll.go", err)
	}

	// Compile the test driver file
	log.Info("Compiling the test driver")
	if err := os.Chdir("../tests/driver"); err != nil {
		log.Errorf("Could not change to directory 'driver':%v", err)
	}
	cmd := exec.Command("go", "build", "-o", outDir+"/runAll")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Errorf("Error running the compile command:%v", err)
	}
	if err := os.Chdir("../../suites"); err != nil {
		log.Errorf("Could not change to directory 'driver':%v",err)
	}

	return nil
}


func main() {

	conf,err := parseCmd()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	// Setup logging
	if _, err := log.SetDefaultLogger(log.JSON, *conf.logLevel, nil); err != nil {
		log.With(log.Fields{"error": err}).Fatal("Cannot setup logging")
	}

	defer log.CleanUp()

	// Parse the config file
	if err := conf.loadConfig(); err != nil {
		log.Error(err)
	}

	generateTestSuites(conf, "../tests", "/src/tests")
	generateTestDriver(conf, "../tests/driver", "/src/tests")
	return
}

