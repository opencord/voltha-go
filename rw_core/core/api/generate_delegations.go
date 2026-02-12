//go:build ignore
// +build ignore

// Copyright 2018-2025 Open Networking Foundation (ONF) and the ONF Contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Generator tool to create delegation methods for APIHandler
// Run with: go run generate_delegations.go
//
// LIMITATIONS & KNOWN ISSUES:
// 1. Complex types: Cannot handle function types, channels, or complex interfaces in signatures
// 2. Package aliases: Hardcoded mapping (ofp -> openflow_13) - add new mappings as needed
// 3. Manager categorization: Uses hardcoded lists - update when methods move between Managers
// 4. Proto evolution: Assumes stable interface names (VolthaServiceServer, CoreServiceServer)
//
// MAINTENANCE REQUIRED WHEN:
// - New proto packages introduced with custom aliases
// - Methods move between Manager/LogicalManager/AdapterManager
// - Proto generator changes file structure or naming
// - New method signatures use unsupported Go types
//
// The generator will FAIL EXPLICITLY if it encounters unsupported patterns.
// Manual review of delegations.go is recommended after regeneration.

package main

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type MethodInfo struct {
	Name       string
	Params     []string
	Results    []string
	ParamNames []string
}

var warnings []string
var errors []string

func addWarning(msg string) {
	warnings = append(warnings, "WARNING: "+msg)
}

func addError(msg string) {
	errors = append(errors, "ERROR: "+msg)
}

func main() {
	// Find vendor directory
	vendorDir := filepath.Join("..", "..", "..", "vendor", "github.com", "opencord", "voltha-protos", "v5", "go")

	// Parse proto interfaces
	protoMethods := make(map[string]bool)
	parseProtoInterface(filepath.Join(vendorDir, "voltha", "voltha_grpc.pb.go"), "VolthaServiceServer", protoMethods)
	parseProtoInterface(filepath.Join(vendorDir, "core_service", "core_services_grpc.pb.go"), "CoreServiceServer", protoMethods)

	// Parse Manager interfaces
	deviceDir := filepath.Join("..", "device")
	managerMethods := parseManagerMethods(deviceDir)

	// Parse adapter.Manager methods
	adapterDir := filepath.Join("..", "adapter")
	adapterMethods := parseAdapterManagerMethods(adapterDir)

	// Merge adapter methods into managerMethods
	for name, info := range adapterMethods {
		managerMethods[name] = info
	}

	// Find overlapping methods
	var overlapping []string
	for method := range protoMethods {
		if managerMethods[method] != nil {
			overlapping = append(overlapping, method)
		}
	}
	sort.Strings(overlapping)

	fmt.Printf("Found %d overlapping methods requiring delegation\n", len(overlapping))
	fmt.Println("Generating delegations.go...")

	// Generate the delegations.go file
	if err := generateDelegationsFile(overlapping, managerMethods); err != nil {
		fmt.Fprintf(os.Stderr, "Error generating delegations.go: %v\n", err)
		os.Exit(1)
	}

	// Format the generated file
	if err := formatFile("delegations.go"); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Could not format delegations.go: %v\n", err)
	}

	// Report warnings and errors
	if len(warnings) > 0 {
		fmt.Println("\nWarnings:")
		for _, w := range warnings {
			fmt.Println("  " + w)
		}
	}

	if len(errors) > 0 {
		fmt.Println("\nErrors:")
		for _, e := range errors {
			fmt.Println("  " + e)
		}
		fmt.Println("\nGenerated code may be incomplete or incorrect!")
		fmt.Println("Manual review and fixes may be required.")
		os.Exit(1)
	}

	fmt.Println("Successfully generated delegations.go")
}

func generateDelegationsFile(methods []string, methodInfo map[string]*MethodInfo) error {
	f, err := os.Create("delegations.go")
	if err != nil {
		return err
	}
	defer f.Close()

	// Write header
	fmt.Fprint(f, `/*
 * Copyright 2018-2023 Open Networking Foundation (ONF) and the ONF Contributors
 * Licensed under the Apache License, Version 2.0 (the "License")
 */

package api

//go:generate go run generate_delegations.go

// This file contains delegation methods to resolve ambiguities between
// embedded UnimplementedVolthaServiceServer/UnimplementedCoreServiceServer
// and the device.Manager that has the actual implementations.
//
// When a method exists in both the Unimplemented stub and the Manager,
// Go cannot determine which one to use, so we explicitly delegate to Manager.
//
// To regenerate this file after proto updates, run: go generate ./rw_core/core/api

import (
	"context"

	"github.com/opencord/voltha-protos/v5/go/common"
	ca "github.com/opencord/voltha-protos/v5/go/core_adapter"
	"github.com/opencord/voltha-protos/v5/go/core_service"
	"github.com/opencord/voltha-protos/v5/go/extension"
	"github.com/opencord/voltha-protos/v5/go/omci"
	"github.com/opencord/voltha-protos/v5/go/openflow_13"
	voip_system_profile "github.com/opencord/voltha-protos/v5/go/voip_system_profile"
	voip_user_profile "github.com/opencord/voltha-protos/v5/go/voip_user_profile"
	"github.com/opencord/voltha-protos/v5/go/voltha"
	"google.golang.org/protobuf/types/known/emptypb"
)

// All methods below delegate to handler.Manager, handler.LogicalManager, or handler.adapterManager
// to resolve ambiguity with embedded UnimplementedVolthaServiceServer/UnimplementedCoreServiceServer

`)

	// Group methods by receiver (determine which Manager they belong to)
	deviceMethods := []string{}
	logicalMethods := []string{}
	adapterMethods := []string{}

	// Known logical device methods
	logicalMethodNames := map[string]bool{
		"DisableLogicalDevicePort": true, "EnableLogicalDevicePort": true,
		"GetLogicalDevice": true, "GetLogicalDevicePort": true,
		"ListLogicalDeviceFlowGroups": true, "ListLogicalDeviceFlows": true,
		"ListLogicalDeviceMeters": true, "ListLogicalDevicePorts": true,
		"ListLogicalDevices": true, "StreamPacketsOut": true,
		"UpdateLogicalDeviceFlowGroupTable": true, "UpdateLogicalDeviceFlowTable": true,
		"UpdateLogicalDeviceMeterTable": true,
	}

	// Known adapter methods
	adapterMethodNames := map[string]bool{
		"GetDeviceType": true, "ListAdapters": true,
		"ListDeviceTypes": true, "RegisterAdapter": true,
	}

	for _, method := range methods {
		if logicalMethodNames[method] {
			logicalMethods = append(logicalMethods, method)
		} else if adapterMethodNames[method] {
			adapterMethods = append(adapterMethods, method)
		} else {
			deviceMethods = append(deviceMethods, method)
		}
	}

	// Warn if method categorization might be wrong
	if len(deviceMethods) > 60 {
		addWarning(fmt.Sprintf("Large number of device methods (%d) - verify Manager categorization", len(deviceMethods)))
	}

	// Write device.Manager delegations
	for _, method := range deviceMethods {
		info := methodInfo[method]
		if info != nil {
			writeDelegation(f, method, info, "Manager")
		}
	}

	// Write LogicalManager delegations
	if len(logicalMethods) > 0 {
		fmt.Fprintln(f, "\n// LogicalManager delegations to resolve ambiguity")
		for _, method := range logicalMethods {
			info := methodInfo[method]
			if info != nil {
				writeDelegation(f, method, info, "LogicalManager")
			}
		}
	}

	// Write adapter.Manager delegations
	if len(adapterMethods) > 0 {
		fmt.Fprintln(f, "\n// adapter.Manager delegations to resolve ambiguity")
		for _, method := range adapterMethods {
			info := methodInfo[method]
			if info != nil {
				writeDelegation(f, method, info, "adapterManager")
			}
		}
	}

	return nil
}

func writeDelegation(f *os.File, methodName string, info *MethodInfo, receiver string) {
	// Build parameter list
	params := []string{}
	for i := range info.ParamNames {
		paramName := info.ParamNames[i]
		// Replace _ with a valid parameter name
		if paramName == "_" {
			paramName = fmt.Sprintf("arg%d", i)
		}
		params = append(params, fmt.Sprintf("%s %s", paramName, info.Params[i]))
	}

	// Build result list
	results := ""
	if len(info.Results) == 1 {
		results = " " + info.Results[0]
	} else if len(info.Results) > 1 {
		results = " (" + strings.Join(info.Results, ", ") + ")"
	}

	// Build call arguments (use original param names for the call)
	callArgs := []string{}
	for i, paramName := range info.ParamNames {
		if paramName == "_" {
			callArgs = append(callArgs, fmt.Sprintf("arg%d", i))
		} else {
			callArgs = append(callArgs, paramName)
		}
	}

	fmt.Fprintf(f, "func (handler *APIHandler) %s(%s)%s {\n", methodName, strings.Join(params, ", "), results)
	if len(info.Results) > 0 {
		fmt.Fprintf(f, "\treturn handler.%s.%s(%s)\n", receiver, methodName, strings.Join(callArgs, ", "))
	} else {
		fmt.Fprintf(f, "\thandler.%s.%s(%s)\n", receiver, methodName, strings.Join(callArgs, ", "))
	}
	fmt.Fprintln(f, "}")
	fmt.Fprintln(f)
}

func parseProtoInterface(filePath, interfaceName string, methods map[string]bool) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filePath, nil, 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Could not parse %s: %v\n", filePath, err)
		return
	}

	ast.Inspect(f, func(n ast.Node) bool {
		typeSpec, ok := n.(*ast.TypeSpec)
		if !ok || typeSpec.Name.Name != interfaceName {
			return true
		}

		iface, ok := typeSpec.Type.(*ast.InterfaceType)
		if !ok {
			return false
		}

		for _, method := range iface.Methods.List {
			if len(method.Names) > 0 {
				methodName := method.Names[0].Name
				if !strings.HasPrefix(methodName, "mustEmbed") {
					methods[methodName] = true
				}
			}
		}
		return false
	})
}

func parseManagerMethods(deviceDir string) map[string]*MethodInfo {
	methods := make(map[string]*MethodInfo)

	// Parse all .go files in the device directory
	dirEntries, err := os.ReadDir(deviceDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Could not read device directory: %v\n", err)
		return methods
	}

	for _, entry := range dirEntries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		filePath := filepath.Join(deviceDir, entry.Name())
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, filePath, nil, 0)
		if err != nil {
			continue
		}

		ast.Inspect(f, func(n ast.Node) bool {
			funcDecl, ok := n.(*ast.FuncDecl)
			if !ok || funcDecl.Recv == nil || len(funcDecl.Recv.List) == 0 {
				return true
			}

			// Check if it's a Manager or LogicalManager method
			recvType := getReceiverType(funcDecl.Recv.List[0].Type)
			if recvType != "Manager" && recvType != "LogicalManager" {
				return true
			}

			methodName := funcDecl.Name.Name
			if !ast.IsExported(methodName) {
				return true
			}

			info := &MethodInfo{
				Name:       methodName,
				Params:     []string{},
				Results:    []string{},
				ParamNames: []string{},
			}

			// Extract parameter types and names
			if funcDecl.Type.Params != nil {
				for i, field := range funcDecl.Type.Params.List {
					typeStr := getTypeString(field.Type)
					if len(field.Names) > 0 {
						for _, name := range field.Names {
							info.Params = append(info.Params, typeStr)
							info.ParamNames = append(info.ParamNames, name.Name)
						}
					} else {
						// Unnamed parameter, generate a name
						paramName := fmt.Sprintf("arg%d", i)
						info.Params = append(info.Params, typeStr)
						info.ParamNames = append(info.ParamNames, paramName)
					}
				}
			}

			// Extract result types
			if funcDecl.Type.Results != nil {
				for _, field := range funcDecl.Type.Results.List {
					typeStr := getTypeString(field.Type)
					// Replace common package aliases with full package names
					typeStr = strings.ReplaceAll(typeStr, "ofp.", "openflow_13.")
					// Warn about potential unknown package aliases
					if strings.Contains(typeStr, "unknown") {
						addWarning(fmt.Sprintf("Method %s has unknown type in results", methodName))
					}
					info.Results = append(info.Results, typeStr)
				}
			}

			methods[methodName] = info
			return true
		})
	}

	return methods
}

func parseAdapterManagerMethods(adapterDir string) map[string]*MethodInfo {
	methods := make(map[string]*MethodInfo)

	// Parse all .go files in the adapter directory
	dirEntries, err := os.ReadDir(adapterDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Could not read adapter directory: %v\n", err)
		return methods
	}

	for _, entry := range dirEntries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		filePath := filepath.Join(adapterDir, entry.Name())
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, filePath, nil, 0)
		if err != nil {
			continue
		}

		ast.Inspect(f, func(n ast.Node) bool {
			funcDecl, ok := n.(*ast.FuncDecl)
			if !ok || funcDecl.Recv == nil || len(funcDecl.Recv.List) == 0 {
				return true
			}

			// Check if it's an adapter.Manager method
			recvType := getReceiverType(funcDecl.Recv.List[0].Type)
			if recvType != "Manager" {
				return true
			}

			methodName := funcDecl.Name.Name
			if !ast.IsExported(methodName) {
				return true
			}

			info := &MethodInfo{
				Name:       methodName,
				Params:     []string{},
				Results:    []string{},
				ParamNames: []string{},
			}

			// Extract parameter types and names
			if funcDecl.Type.Params != nil {
				for i, field := range funcDecl.Type.Params.List {
					typeStr := getTypeString(field.Type)
					// Replace common package aliases with full package names
					typeStr = strings.ReplaceAll(typeStr, "core_adapter.", "ca.")
					if len(field.Names) > 0 {
						for _, name := range field.Names {
							info.Params = append(info.Params, typeStr)
							info.ParamNames = append(info.ParamNames, name.Name)
						}
					} else {
						// Unnamed parameter, generate a name
						paramName := fmt.Sprintf("arg%d", i)
						info.Params = append(info.Params, typeStr)
						info.ParamNames = append(info.ParamNames, paramName)
					}
				}
			}

			// Extract result types
			if funcDecl.Type.Results != nil {
				for _, field := range funcDecl.Type.Results.List {
					typeStr := getTypeString(field.Type)
					// Replace common package aliases with full package names
					typeStr = strings.ReplaceAll(typeStr, "ofp.", "openflow_13.")
					typeStr = strings.ReplaceAll(typeStr, "core_adapter.", "ca.")
					// Warn about potential unknown package aliases
					if strings.Contains(typeStr, "unknown") {
						addWarning(fmt.Sprintf("Method %s has unknown type in results", methodName))
					}
					info.Results = append(info.Results, typeStr)
				}
			}

			methods[methodName] = info
			return true
		})
	}

	return methods
}

func getReceiverType(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		if ident, ok := t.X.(*ast.Ident); ok {
			return ident.Name
		}
	case *ast.Ident:
		return t.Name
	}
	return ""
}

func getTypeString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + getTypeString(t.X)
	case *ast.SelectorExpr:
		return getTypeString(t.X) + "." + t.Sel.Name
	case *ast.ArrayType:
		return "[]" + getTypeString(t.Elt)
	case *ast.MapType:
		return "map[" + getTypeString(t.Key) + "]" + getTypeString(t.Value)
	case *ast.InterfaceType:
		if t.Methods == nil || len(t.Methods.List) == 0 {
			return "interface{}"
		}
		addWarning("Complex interface type detected - may need manual review")
		return "interface{}"
	case *ast.Ellipsis:
		return "..." + getTypeString(t.Elt)
	case *ast.FuncType:
		addError("Function type parameter detected - not supported by generator")
		return "func(...) ..."
	case *ast.ChanType:
		addError("Channel type parameter detected - not supported by generator")
		return "chan ..."
	default:
		addError(fmt.Sprintf("Unknown type expression: %T", expr))
		return "unknown"
	}
}

func formatFile(filename string) error {
	// Read the file
	content, err := os.ReadFile(filename)
	if err != nil {
		return err
	}

	// Parse and format
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filename, content, parser.ParseComments)
	if err != nil {
		return err
	}

	// Write formatted output
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	return format.Node(f, fset, node)
}
