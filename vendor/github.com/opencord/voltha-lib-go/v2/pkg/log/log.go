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

//Package log provides a structured Logger interface implemented using zap logger. It provides the following capabilities:
//1. Package level logging - a go package can register itself (AddPackage) and have a logger created for that package.
//2. Dynamic log level change - for all registered packages (SetAllLogLevel)
//3. Dynamic log level change - for a given package (SetPackageLogLevel)
//4. Provides a default logger for unregistered packages
//5. Allow key-value pairs to be added to a logger(UpdateLogger) or all loggers (UpdateAllLoggers) at run time
//6. Add to the log output the location where the log was invoked (filename.functionname.linenumber)
//
// Using package-level logging (recommended approach).  In the examples below, log refers to this log package.
// 1.  In the appropriate package add the following in the init section of the package.  The log level can be changed
// and any number of default fields can be added as well. The log level specifies the lowest log level that will be
// in the output while the fields will be automatically added to all log printouts.
//
//	log.AddPackage(mylog.JSON, log.WarnLevel, log.Fields{"anyFieldName": "any value"})
//
//2. In the calling package, just invoke any of the publicly available functions of the logger.  Here is an  example
// to write an Info log with additional fields:
//
//log.Infow("An example", mylog.Fields{"myStringOutput": "output", "myIntOutput": 2})
//
//3. To dynamically change the log level, you can use 1)SetLogLevel from inside your package or 2) SetPackageLogLevel
// from anywhere or 3)  SetAllLogLevel from anywhere.
//

package log

import (
	"errors"
	"fmt"
	zp "go.uber.org/zap"
	zc "go.uber.org/zap/zapcore"
	"path"
	"runtime"
	"strings"
)

const (
	// DebugLevel logs a message at debug level
	DebugLevel = iota
	// InfoLevel logs a message at info level
	InfoLevel
	// WarnLevel logs a message at warning level
	WarnLevel
	// ErrorLevel logs a message at error level
	ErrorLevel
	// FatalLevel logs a message, then calls os.Exit(1).
	FatalLevel
)

// CONSOLE formats the log for the console, mostly used during development
const CONSOLE = "console"

// JSON formats the log using json format, mostly used by an automated logging system consumption
const JSON = "json"

// Logger represents an abstract logging interface.  Any logging implementation used
// will need to abide by this interface
type Logger interface {
	Debug(...interface{})
	Debugln(...interface{})
	Debugf(string, ...interface{})
	Debugw(string, Fields)

	Info(...interface{})
	Infoln(...interface{})
	Infof(string, ...interface{})
	Infow(string, Fields)

	Warn(...interface{})
	Warnln(...interface{})
	Warnf(string, ...interface{})
	Warnw(string, Fields)

	Error(...interface{})
	Errorln(...interface{})
	Errorf(string, ...interface{})
	Errorw(string, Fields)

	Fatal(...interface{})
	Fatalln(...interface{})
	Fatalf(string, ...interface{})
	Fatalw(string, Fields)

	With(Fields) Logger

	// The following are added to be able to use this logger as a gRPC LoggerV2 if needed
	//
	Warning(...interface{})
	Warningln(...interface{})
	Warningf(string, ...interface{})

	// V reports whether verbosity level l is at least the requested verbose level.
	V(l int) bool

	//Returns the log level of this specific logger
	GetLogLevel() int
}

// Fields is used as key-value pairs for structured logging
type Fields map[string]interface{}

var defaultLogger *logger
var cfg zp.Config

var loggers map[string]*logger
var cfgs map[string]zp.Config

type logger struct {
	log         *zp.SugaredLogger
	parent      *zp.Logger
	packageName string
}

func intToAtomicLevel(l int) zp.AtomicLevel {
	switch l {
	case DebugLevel:
		return zp.NewAtomicLevelAt(zc.DebugLevel)
	case InfoLevel:
		return zp.NewAtomicLevelAt(zc.InfoLevel)
	case WarnLevel:
		return zp.NewAtomicLevelAt(zc.WarnLevel)
	case ErrorLevel:
		return zp.NewAtomicLevelAt(zc.ErrorLevel)
	case FatalLevel:
		return zp.NewAtomicLevelAt(zc.FatalLevel)
	}
	return zp.NewAtomicLevelAt(zc.ErrorLevel)
}

func intToLevel(l int) zc.Level {
	switch l {
	case DebugLevel:
		return zc.DebugLevel
	case InfoLevel:
		return zc.InfoLevel
	case WarnLevel:
		return zc.WarnLevel
	case ErrorLevel:
		return zc.ErrorLevel
	case FatalLevel:
		return zc.FatalLevel
	}
	return zc.ErrorLevel
}

func levelToInt(l zc.Level) int {
	switch l {
	case zc.DebugLevel:
		return DebugLevel
	case zc.InfoLevel:
		return InfoLevel
	case zc.WarnLevel:
		return WarnLevel
	case zc.ErrorLevel:
		return ErrorLevel
	case zc.FatalLevel:
		return FatalLevel
	}
	return ErrorLevel
}

func StringToInt(l string) int {
	switch l {
	case "DEBUG":
		return DebugLevel
	case "INFO":
		return InfoLevel
	case "WARN":
		return WarnLevel
	case "ERROR":
		return ErrorLevel
	case "FATAL":
		return FatalLevel
	}
	return ErrorLevel
}

func getDefaultConfig(outputType string, level int, defaultFields Fields) zp.Config {
	return zp.Config{
		Level:            intToAtomicLevel(level),
		Encoding:         outputType,
		Development:      true,
		OutputPaths:      []string{"stdout"},
		ErrorOutputPaths: []string{"stderr"},
		InitialFields:    defaultFields,
		EncoderConfig: zc.EncoderConfig{
			LevelKey:       "level",
			MessageKey:     "msg",
			TimeKey:        "ts",
			StacktraceKey:  "stacktrace",
			LineEnding:     zc.DefaultLineEnding,
			EncodeLevel:    zc.LowercaseLevelEncoder,
			EncodeTime:     zc.ISO8601TimeEncoder,
			EncodeDuration: zc.SecondsDurationEncoder,
			EncodeCaller:   zc.ShortCallerEncoder,
		},
	}
}

// SetLogger needs to be invoked before the logger API can be invoked.  This function
// initialize the default logger (zap's sugaredlogger)
func SetDefaultLogger(outputType string, level int, defaultFields Fields) (Logger, error) {
	// Build a custom config using zap
	cfg = getDefaultConfig(outputType, level, defaultFields)

	l, err := cfg.Build()
	if err != nil {
		return nil, err
	}

	defaultLogger = &logger{
		log:    l.Sugar(),
		parent: l,
	}

	return defaultLogger, nil
}

// AddPackage registers a package to the log map.  Each package gets its own logger which allows
// its config (loglevel) to be changed dynamically without interacting with the other packages.
// outputType is JSON, level is the lowest level log to output with this logger and defaultFields is a map of
// key-value pairs to always add to the output.
// Note: AddPackage also returns a reference to the actual logger.  If a calling package uses this reference directly
//instead of using the publicly available functions in this log package then a number of functionalities will not
// be available to it, notably log tracing with filename.functionname.linenumber annotation.
//
// pkgNames parameter should be used for testing only as this function detects the caller's package.
func AddPackage(outputType string, level int, defaultFields Fields, pkgNames ...string) (Logger, error) {
	if cfgs == nil {
		cfgs = make(map[string]zp.Config)
	}
	if loggers == nil {
		loggers = make(map[string]*logger)
	}

	var pkgName string
	for _, name := range pkgNames {
		pkgName = name
		break
	}
	if pkgName == "" {
		pkgName, _, _, _ = getCallerInfo()
	}

	if _, exist := loggers[pkgName]; exist {
		return loggers[pkgName], nil
	}

	cfgs[pkgName] = getDefaultConfig(outputType, level, defaultFields)

	l, err := cfgs[pkgName].Build()
	if err != nil {
		return nil, err
	}

	loggers[pkgName] = &logger{
		log:         l.Sugar(),
		parent:      l,
		packageName: pkgName,
	}
	return loggers[pkgName], nil
}

//UpdateAllLoggers create new loggers for all registered pacakges with the defaultFields.
func UpdateAllLoggers(defaultFields Fields) error {
	for pkgName, cfg := range cfgs {
		for k, v := range defaultFields {
			if cfg.InitialFields == nil {
				cfg.InitialFields = make(map[string]interface{})
			}
			cfg.InitialFields[k] = v
		}
		l, err := cfg.Build()
		if err != nil {
			return err
		}

		loggers[pkgName] = &logger{
			log:         l.Sugar(),
			parent:      l,
			packageName: pkgName,
		}
	}
	return nil
}

// Return a list of all packages that have individually-configured loggers
func GetPackageNames() []string {
	i := 0
	keys := make([]string, len(loggers))
	for k := range loggers {
		keys[i] = k
		i++
	}
	return keys
}

// UpdateLogger deletes the logger associated with a caller's package and creates a new logger with the
// defaultFields.  If a calling package is holding on to a Logger reference obtained from AddPackage invocation, then
// that package needs to invoke UpdateLogger if it needs to make changes to the default fields and obtain a new logger
// reference
func UpdateLogger(defaultFields Fields) (Logger, error) {
	pkgName, _, _, _ := getCallerInfo()
	if _, exist := loggers[pkgName]; !exist {
		return nil, errors.New(fmt.Sprintf("package-%s-not-registered", pkgName))
	}

	// Build a new logger
	if _, exist := cfgs[pkgName]; !exist {
		return nil, errors.New(fmt.Sprintf("config-%s-not-registered", pkgName))
	}

	cfg := cfgs[pkgName]
	for k, v := range defaultFields {
		if cfg.InitialFields == nil {
			cfg.InitialFields = make(map[string]interface{})
		}
		cfg.InitialFields[k] = v
	}
	l, err := cfg.Build()
	if err != nil {
		return nil, err
	}

	// Set the logger
	loggers[pkgName] = &logger{
		log:         l.Sugar(),
		parent:      l,
		packageName: pkgName,
	}
	return loggers[pkgName], nil
}

func setLevel(cfg zp.Config, level int) {
	switch level {
	case DebugLevel:
		cfg.Level.SetLevel(zc.DebugLevel)
	case InfoLevel:
		cfg.Level.SetLevel(zc.InfoLevel)
	case WarnLevel:
		cfg.Level.SetLevel(zc.WarnLevel)
	case ErrorLevel:
		cfg.Level.SetLevel(zc.ErrorLevel)
	case FatalLevel:
		cfg.Level.SetLevel(zc.FatalLevel)
	default:
		cfg.Level.SetLevel(zc.ErrorLevel)
	}
}

//SetPackageLogLevel dynamically sets the log level of a given package to level.  This is typically invoked at an
// application level during debugging
func SetPackageLogLevel(packageName string, level int) {
	// Get proper config
	if cfg, ok := cfgs[packageName]; ok {
		setLevel(cfg, level)
	}
}

//SetAllLogLevel sets the log level of all registered packages to level
func SetAllLogLevel(level int) {
	// Get proper config
	for _, cfg := range cfgs {
		setLevel(cfg, level)
	}
}

//GetPackageLogLevel returns the current log level of a package.
func GetPackageLogLevel(packageName ...string) (int, error) {
	var name string
	if len(packageName) == 1 {
		name = packageName[0]
	} else {
		name, _, _, _ = getCallerInfo()
	}
	if cfg, ok := cfgs[name]; ok {
		return levelToInt(cfg.Level.Level()), nil
	}
	return 0, errors.New(fmt.Sprintf("unknown-package-%s", name))
}

//GetDefaultLogLevel gets the log level used for packages that don't have specific loggers
func GetDefaultLogLevel() int {
	return levelToInt(cfg.Level.Level())
}

//SetLogLevel sets the log level for the logger corresponding to the caller's package
func SetLogLevel(level int) error {
	pkgName, _, _, _ := getCallerInfo()
	if _, exist := cfgs[pkgName]; !exist {
		return errors.New(fmt.Sprintf("unregistered-package-%s", pkgName))
	}
	cfg := cfgs[pkgName]
	setLevel(cfg, level)
	return nil
}

//SetDefaultLogLevel sets the log level used for packages that don't have specific loggers
func SetDefaultLogLevel(level int) {
	setLevel(cfg, level)
}

// CleanUp flushed any buffered log entries. Applications should take care to call
// CleanUp before exiting.
func CleanUp() error {
	for _, logger := range loggers {
		if logger != nil {
			if logger.parent != nil {
				if err := logger.parent.Sync(); err != nil {
					return err
				}
			}
		}
	}
	if defaultLogger != nil {
		if defaultLogger.parent != nil {
			if err := defaultLogger.parent.Sync(); err != nil {
				return err
			}
		}
	}
	return nil
}

func getCallerInfo() (string, string, string, int) {
	// Since the caller of a log function is one stack frame before (in terms of stack higher level) the log.go
	// filename, then first look for the last log.go filename and then grab the caller info one level higher.
	maxLevel := 3
	skiplevel := 3 // Level with the most empirical success to see the last log.go stack frame.
	pc := make([]uintptr, maxLevel)
	n := runtime.Callers(skiplevel, pc)
	packageName := ""
	funcName := ""
	fileName := ""
	var line int
	if n == 0 {
		return packageName, fileName, funcName, line
	}
	frames := runtime.CallersFrames(pc[:n])
	var frame runtime.Frame
	var foundFrame runtime.Frame
	more := true
	for more {
		frame, more = frames.Next()
		_, fileName = path.Split(frame.File)
		if fileName != "log.go" {
			foundFrame = frame // First frame after log.go in the frame stack
			break
		}
	}
	parts := strings.Split(foundFrame.Function, ".")
	pl := len(parts)
	if pl >= 2 {
		funcName = parts[pl-1]
		if parts[pl-2][0] == '(' {
			packageName = strings.Join(parts[0:pl-2], ".")
		} else {
			packageName = strings.Join(parts[0:pl-1], ".")
		}
	}

	if strings.HasSuffix(packageName, ".init") {
		packageName = strings.TrimSuffix(packageName, ".init")
	}

	if strings.HasSuffix(fileName, ".go") {
		fileName = strings.TrimSuffix(fileName, ".go")
	}

	return packageName, fileName, funcName, foundFrame.Line
}

func getPackageLevelSugaredLogger() *zp.SugaredLogger {
	pkgName, fileName, funcName, line := getCallerInfo()
	if _, exist := loggers[pkgName]; exist {
		return loggers[pkgName].log.With("caller", fmt.Sprintf("%s.%s:%d", fileName, funcName, line))
	}
	return defaultLogger.log.With("caller", fmt.Sprintf("%s.%s:%d", fileName, funcName, line))
}

func getPackageLevelLogger() Logger {
	pkgName, _, _, _ := getCallerInfo()
	if _, exist := loggers[pkgName]; exist {
		return loggers[pkgName]
	}
	return defaultLogger
}

func serializeMap(fields Fields) []interface{} {
	data := make([]interface{}, len(fields)*2)
	i := 0
	for k, v := range fields {
		data[i] = k
		data[i+1] = v
		i = i + 2
	}
	return data
}

// With returns a logger initialized with the key-value pairs
func (l logger) With(keysAndValues Fields) Logger {
	return logger{log: l.log.With(serializeMap(keysAndValues)...), parent: l.parent}
}

// Debug logs a message at level Debug on the standard logger.
func (l logger) Debug(args ...interface{}) {
	l.log.Debug(args...)
}

// Debugln logs a message at level Debug on the standard logger with a line feed. Default in any case.
func (l logger) Debugln(args ...interface{}) {
	l.log.Debug(args...)
}

// Debugw logs a message at level Debug on the standard logger.
func (l logger) Debugf(format string, args ...interface{}) {
	l.log.Debugf(format, args...)
}

// Debugw logs a message with some additional context. The variadic key-value
// pairs are treated as they are in With.
func (l logger) Debugw(msg string, keysAndValues Fields) {
	l.log.Debugw(msg, serializeMap(keysAndValues)...)
}

// Info logs a message at level Info on the standard logger.
func (l logger) Info(args ...interface{}) {
	l.log.Info(args...)
}

// Infoln logs a message at level Info on the standard logger with a line feed. Default in any case.
func (l logger) Infoln(args ...interface{}) {
	l.log.Info(args...)
	//msg := fmt.Sprintln(args...)
	//l.sourced().Info(msg[:len(msg)-1])
}

// Infof logs a message at level Info on the standard logger.
func (l logger) Infof(format string, args ...interface{}) {
	l.log.Infof(format, args...)
}

// Infow logs a message with some additional context. The variadic key-value
// pairs are treated as they are in With.
func (l logger) Infow(msg string, keysAndValues Fields) {
	l.log.Infow(msg, serializeMap(keysAndValues)...)
}

// Warn logs a message at level Warn on the standard logger.
func (l logger) Warn(args ...interface{}) {
	l.log.Warn(args...)
}

// Warnln logs a message at level Warn on the standard logger with a line feed. Default in any case.
func (l logger) Warnln(args ...interface{}) {
	l.log.Warn(args...)
}

// Warnf logs a message at level Warn on the standard logger.
func (l logger) Warnf(format string, args ...interface{}) {
	l.log.Warnf(format, args...)
}

// Warnw logs a message with some additional context. The variadic key-value
// pairs are treated as they are in With.
func (l logger) Warnw(msg string, keysAndValues Fields) {
	l.log.Warnw(msg, serializeMap(keysAndValues)...)
}

// Error logs a message at level Error on the standard logger.
func (l logger) Error(args ...interface{}) {
	l.log.Error(args...)
}

// Errorln logs a message at level Error on the standard logger with a line feed. Default in any case.
func (l logger) Errorln(args ...interface{}) {
	l.log.Error(args...)
}

// Errorf logs a message at level Error on the standard logger.
func (l logger) Errorf(format string, args ...interface{}) {
	l.log.Errorf(format, args...)
}

// Errorw logs a message with some additional context. The variadic key-value
// pairs are treated as they are in With.
func (l logger) Errorw(msg string, keysAndValues Fields) {
	l.log.Errorw(msg, serializeMap(keysAndValues)...)
}

// Fatal logs a message at level Fatal on the standard logger.
func (l logger) Fatal(args ...interface{}) {
	l.log.Fatal(args...)
}

// Fatalln logs a message at level Fatal on the standard logger with a line feed. Default in any case.
func (l logger) Fatalln(args ...interface{}) {
	l.log.Fatal(args...)
}

// Fatalf logs a message at level Fatal on the standard logger.
func (l logger) Fatalf(format string, args ...interface{}) {
	l.log.Fatalf(format, args...)
}

// Fatalw logs a message with some additional context. The variadic key-value
// pairs are treated as they are in With.
func (l logger) Fatalw(msg string, keysAndValues Fields) {
	l.log.Fatalw(msg, serializeMap(keysAndValues)...)
}

// Warning logs a message at level Warn on the standard logger.
func (l logger) Warning(args ...interface{}) {
	l.log.Warn(args...)
}

// Warningln logs a message at level Warn on the standard logger with a line feed. Default in any case.
func (l logger) Warningln(args ...interface{}) {
	l.log.Warn(args...)
}

// Warningf logs a message at level Warn on the standard logger.
func (l logger) Warningf(format string, args ...interface{}) {
	l.log.Warnf(format, args...)
}

// V reports whether verbosity level l is at least the requested verbose level.
func (l logger) V(level int) bool {
	return l.parent.Core().Enabled(intToLevel(level))
}

// GetLogLevel returns the current level of the logger
func (l logger) GetLogLevel() int {
	return levelToInt(cfgs[l.packageName].Level.Level())
}

// With returns a logger initialized with the key-value pairs
func With(keysAndValues Fields) Logger {
	return logger{log: getPackageLevelSugaredLogger().With(serializeMap(keysAndValues)...), parent: defaultLogger.parent}
}

// Debug logs a message at level Debug on the standard logger.
func Debug(args ...interface{}) {
	getPackageLevelSugaredLogger().Debug(args...)
}

// Debugln logs a message at level Debug on the standard logger.
func Debugln(args ...interface{}) {
	getPackageLevelSugaredLogger().Debug(args...)
}

// Debugf logs a message at level Debug on the standard logger.
func Debugf(format string, args ...interface{}) {
	getPackageLevelSugaredLogger().Debugf(format, args...)
}

// Debugw logs a message with some additional context. The variadic key-value
// pairs are treated as they are in With.
func Debugw(msg string, keysAndValues Fields) {
	getPackageLevelSugaredLogger().Debugw(msg, serializeMap(keysAndValues)...)
}

// Info logs a message at level Info on the standard logger.
func Info(args ...interface{}) {
	getPackageLevelSugaredLogger().Info(args...)
}

// Infoln logs a message at level Info on the standard logger.
func Infoln(args ...interface{}) {
	getPackageLevelSugaredLogger().Info(args...)
}

// Infof logs a message at level Info on the standard logger.
func Infof(format string, args ...interface{}) {
	getPackageLevelSugaredLogger().Infof(format, args...)
}

//Infow logs a message with some additional context. The variadic key-value
//pairs are treated as they are in With.
func Infow(msg string, keysAndValues Fields) {
	getPackageLevelSugaredLogger().Infow(msg, serializeMap(keysAndValues)...)
}

// Warn logs a message at level Warn on the standard logger.
func Warn(args ...interface{}) {
	getPackageLevelSugaredLogger().Warn(args...)
}

// Warnln logs a message at level Warn on the standard logger.
func Warnln(args ...interface{}) {
	getPackageLevelSugaredLogger().Warn(args...)
}

// Warnf logs a message at level Warn on the standard logger.
func Warnf(format string, args ...interface{}) {
	getPackageLevelSugaredLogger().Warnf(format, args...)
}

// Warnw logs a message with some additional context. The variadic key-value
// pairs are treated as they are in With.
func Warnw(msg string, keysAndValues Fields) {
	getPackageLevelSugaredLogger().Warnw(msg, serializeMap(keysAndValues)...)
}

// Error logs a message at level Error on the standard logger.
func Error(args ...interface{}) {
	getPackageLevelSugaredLogger().Error(args...)
}

// Errorln logs a message at level Error on the standard logger.
func Errorln(args ...interface{}) {
	getPackageLevelSugaredLogger().Error(args...)
}

// Errorf logs a message at level Error on the standard logger.
func Errorf(format string, args ...interface{}) {
	getPackageLevelSugaredLogger().Errorf(format, args...)
}

// Errorw logs a message with some additional context. The variadic key-value
// pairs are treated as they are in With.
func Errorw(msg string, keysAndValues Fields) {
	getPackageLevelSugaredLogger().Errorw(msg, serializeMap(keysAndValues)...)
}

// Fatal logs a message at level Fatal on the standard logger.
func Fatal(args ...interface{}) {
	getPackageLevelSugaredLogger().Fatal(args...)
}

// Fatalln logs a message at level Fatal on the standard logger.
func Fatalln(args ...interface{}) {
	getPackageLevelSugaredLogger().Fatal(args...)
}

// Fatalf logs a message at level Fatal on the standard logger.
func Fatalf(format string, args ...interface{}) {
	getPackageLevelSugaredLogger().Fatalf(format, args...)
}

// Fatalw logs a message with some additional context. The variadic key-value
// pairs are treated as they are in With.
func Fatalw(msg string, keysAndValues Fields) {
	getPackageLevelSugaredLogger().Fatalw(msg, serializeMap(keysAndValues)...)
}

// Warning logs a message at level Warn on the standard logger.
func Warning(args ...interface{}) {
	getPackageLevelSugaredLogger().Warn(args...)
}

// Warningln logs a message at level Warn on the standard logger.
func Warningln(args ...interface{}) {
	getPackageLevelSugaredLogger().Warn(args...)
}

// Warningf logs a message at level Warn on the standard logger.
func Warningf(format string, args ...interface{}) {
	getPackageLevelSugaredLogger().Warnf(format, args...)
}

// V reports whether verbosity level l is at least the requested verbose level.
func V(level int) bool {
	return getPackageLevelLogger().V(level)
}

//GetLogLevel returns the log level of the invoking package
func GetLogLevel() int {
	return getPackageLevelLogger().GetLogLevel()
}
