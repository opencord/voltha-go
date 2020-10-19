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

// Package log provides a structured Logger interface implemented using zap logger. It provides the following capabilities:
// 1. Package level logging - a go package can register itself (AddPackage) and have a logger created for that package.
// 2. Dynamic log level change - for all registered packages (SetAllLogLevel)
// 3. Dynamic log level change - for a given package (SetPackageLogLevel)
// 4. Provides a default logger for unregistered packages (however avoid its usage)
// 5. Allow key-value pairs to be added to a logger(UpdateLogger) or all loggers (UpdateAllLoggers) at run time
// 6. Add to the log output the location where the log was invoked (filename.functionname.linenumber)
//
// Using package-level logging (recommended approach).  In the examples below, log refers to this log package.
//
// 1. In the appropriate package, add the following in the init section of the package (usually in a common.go file)
//    The log level can be changed and any number of default fields can be added as well. The log level specifies
//    the lowest log level that will be in the output while the fields will be automatically added to all log printouts.
//    However, as voltha components re-initialize the log level of each registered package to default initial loglevel
//    passed as CLI argument, the log level passed in RegisterPackage call effectively has no effect.
//
//    var logger log.CLogger
//    func init() {
//              logger, err = log.RegisterPackage(log.JSON, log.ErrorLevel, log.Fields{"key1": "value1"})
//    }
//
// 2. In the calling package, use any of the publicly available functions of local package-level logger instance created
//    in previous step.  Here is an example to write an Info log with additional fields:
//
//    logger.Infow("An example", mylog.Fields{"myStringOutput": "output", "myIntOutput": 2})
//
// 3. To dynamically change the log level, you can use
//          a) SetLogLevel from inside your package or
//          b) SetPackageLogLevel from anywhere or
//          c) SetAllLogLevel from anywhere.
//
//    Dynamic Loglevel configuration feature also uses SetPackageLogLevel method based on triggers received due to
//    Changes to configured loglevels

package log

import (
	"context"
	"errors"
	"fmt"
	zp "go.uber.org/zap"
	zc "go.uber.org/zap/zapcore"
	"path"
	"runtime"
	"strings"
)

type LogLevel int8

const (
	// DebugLevel logs a message at debug level
	DebugLevel = LogLevel(iota)
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

// Context Aware Logger represents an abstract logging interface.  Any logging implementation used
// will need to abide by this interface
type CLogger interface {
	Debug(context.Context, ...interface{})
	Debugln(context.Context, ...interface{})
	Debugf(context.Context, string, ...interface{})
	Debugw(context.Context, string, Fields)

	Info(context.Context, ...interface{})
	Infoln(context.Context, ...interface{})
	Infof(context.Context, string, ...interface{})
	Infow(context.Context, string, Fields)

	Warn(context.Context, ...interface{})
	Warnln(context.Context, ...interface{})
	Warnf(context.Context, string, ...interface{})
	Warnw(context.Context, string, Fields)

	Error(context.Context, ...interface{})
	Errorln(context.Context, ...interface{})
	Errorf(context.Context, string, ...interface{})
	Errorw(context.Context, string, Fields)

	Fatal(context.Context, ...interface{})
	Fatalln(context.Context, ...interface{})
	Fatalf(context.Context, string, ...interface{})
	Fatalw(context.Context, string, Fields)

	With(Fields) CLogger

	// The following are added to be able to use this logger as a gRPC LoggerV2 if needed
	//
	Warning(context.Context, ...interface{})
	Warningln(context.Context, ...interface{})
	Warningf(context.Context, string, ...interface{})

	// V reports whether verbosity level l is at least the requested verbose level.
	V(l LogLevel) bool

	//Returns the log level of this specific logger
	GetLogLevel() LogLevel
}

// Fields is used as key-value pairs for structured logging
type Fields map[string]interface{}

var defaultLogger *clogger
var cfg zp.Config

var loggers map[string]*clogger
var cfgs map[string]zp.Config

type clogger struct {
	log         *zp.SugaredLogger
	parent      *zp.Logger
	packageName string
}

func logLevelToAtomicLevel(l LogLevel) zp.AtomicLevel {
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

func logLevelToLevel(l LogLevel) zc.Level {
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

func levelToLogLevel(l zc.Level) LogLevel {
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

func StringToLogLevel(l string) (LogLevel, error) {
	switch strings.ToUpper(l) {
	case "DEBUG":
		return DebugLevel, nil
	case "INFO":
		return InfoLevel, nil
	case "WARN":
		return WarnLevel, nil
	case "ERROR":
		return ErrorLevel, nil
	case "FATAL":
		return FatalLevel, nil
	}
	return 0, errors.New("Given LogLevel is invalid : " + l)
}

func LogLevelToString(l LogLevel) (string, error) {
	switch l {
	case DebugLevel:
		return "DEBUG", nil
	case InfoLevel:
		return "INFO", nil
	case WarnLevel:
		return "WARN", nil
	case ErrorLevel:
		return "ERROR", nil
	case FatalLevel:
		return "FATAL", nil
	}
	return "", errors.New("Given LogLevel is invalid " + string(l))
}

func getDefaultConfig(outputType string, level LogLevel, defaultFields Fields) zp.Config {
	return zp.Config{
		Level:            logLevelToAtomicLevel(level),
		Encoding:         outputType,
		Development:      true,
		OutputPaths:      []string{"stdout"},
		ErrorOutputPaths: []string{"stderr"},
		InitialFields:    defaultFields,
		EncoderConfig: zc.EncoderConfig{
			LevelKey:       "level",
			MessageKey:     "msg",
			TimeKey:        "ts",
			CallerKey:      "caller",
			StacktraceKey:  "stacktrace",
			LineEnding:     zc.DefaultLineEnding,
			EncodeLevel:    zc.LowercaseLevelEncoder,
			EncodeTime:     zc.ISO8601TimeEncoder,
			EncodeDuration: zc.SecondsDurationEncoder,
			EncodeCaller:   zc.ShortCallerEncoder,
		},
	}
}

func ConstructZapConfig(outputType string, level LogLevel, fields Fields) zp.Config {
	return getDefaultConfig(outputType, level, fields)
}

// SetLogger needs to be invoked before the logger API can be invoked.  This function
// initialize the default logger (zap's sugaredlogger)
func SetDefaultLogger(outputType string, level LogLevel, defaultFields Fields) (CLogger, error) {
	// Build a custom config using zap
	cfg = getDefaultConfig(outputType, level, defaultFields)

	l, err := cfg.Build(zp.AddCallerSkip(1))
	if err != nil {
		return nil, err
	}

	defaultLogger = &clogger{
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
func RegisterPackage(outputType string, level LogLevel, defaultFields Fields, pkgNames ...string) (CLogger, error) {
	if cfgs == nil {
		cfgs = make(map[string]zp.Config)
	}
	if loggers == nil {
		loggers = make(map[string]*clogger)
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

	l, err := cfgs[pkgName].Build(zp.AddCallerSkip(1))
	if err != nil {
		return nil, err
	}

	loggers[pkgName] = &clogger{
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
		l, err := cfg.Build(zp.AddCallerSkip(1))
		if err != nil {
			return err
		}

		// Update the existing zap logger instance
		loggers[pkgName].log = l.Sugar()
		loggers[pkgName].parent = l
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

// UpdateLogger updates the logger associated with a caller's package with supplied defaultFields
func UpdateLogger(defaultFields Fields) error {
	pkgName, _, _, _ := getCallerInfo()
	if _, exist := loggers[pkgName]; !exist {
		return fmt.Errorf("package-%s-not-registered", pkgName)
	}

	// Build a new logger
	if _, exist := cfgs[pkgName]; !exist {
		return fmt.Errorf("config-%s-not-registered", pkgName)
	}

	cfg := cfgs[pkgName]
	for k, v := range defaultFields {
		if cfg.InitialFields == nil {
			cfg.InitialFields = make(map[string]interface{})
		}
		cfg.InitialFields[k] = v
	}
	l, err := cfg.Build(zp.AddCallerSkip(1))
	if err != nil {
		return err
	}

	// Update the existing zap logger instance
	loggers[pkgName].log = l.Sugar()
	loggers[pkgName].parent = l

	return nil
}

func setLevel(cfg zp.Config, level LogLevel) {
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
func SetPackageLogLevel(packageName string, level LogLevel) {
	// Get proper config
	if cfg, ok := cfgs[packageName]; ok {
		setLevel(cfg, level)
	}
}

//SetAllLogLevel sets the log level of all registered packages to level
func SetAllLogLevel(level LogLevel) {
	// Get proper config
	for _, cfg := range cfgs {
		setLevel(cfg, level)
	}
}

//GetPackageLogLevel returns the current log level of a package.
func GetPackageLogLevel(packageName ...string) (LogLevel, error) {
	var name string
	if len(packageName) == 1 {
		name = packageName[0]
	} else {
		name, _, _, _ = getCallerInfo()
	}
	if cfg, ok := cfgs[name]; ok {
		return levelToLogLevel(cfg.Level.Level()), nil
	}
	return 0, fmt.Errorf("unknown-package-%s", name)
}

//GetDefaultLogLevel gets the log level used for packages that don't have specific loggers
func GetDefaultLogLevel() LogLevel {
	return levelToLogLevel(cfg.Level.Level())
}

//SetLogLevel sets the log level for the logger corresponding to the caller's package
func SetLogLevel(level LogLevel) error {
	pkgName, _, _, _ := getCallerInfo()
	if _, exist := cfgs[pkgName]; !exist {
		return fmt.Errorf("unregistered-package-%s", pkgName)
	}
	cfg := cfgs[pkgName]
	setLevel(cfg, level)
	return nil
}

//SetDefaultLogLevel sets the log level used for packages that don't have specific loggers
func SetDefaultLogLevel(level LogLevel) {
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

// With returns a logger initialized with the key-value pairs
func (l clogger) With(keysAndValues Fields) CLogger {
	return clogger{log: l.log.With(serializeMap(keysAndValues)...), parent: l.parent}
}

// Debug logs a message at level Debug on the standard logger.
func (l clogger) Debug(ctx context.Context, args ...interface{}) {
	l.log.With(GetGlobalLFM().ExtractContextAttributes(ctx)...).Debug(args...)
}

// Debugln logs a message at level Debug on the standard logger with a line feed. Default in any case.
func (l clogger) Debugln(ctx context.Context, args ...interface{}) {
	l.log.With(GetGlobalLFM().ExtractContextAttributes(ctx)...).Debug(args...)
}

// Debugw logs a message at level Debug on the standard logger.
func (l clogger) Debugf(ctx context.Context, format string, args ...interface{}) {
	l.log.With(GetGlobalLFM().ExtractContextAttributes(ctx)...).Debugf(format, args...)
}

// Debugw logs a message with some additional context. The variadic key-value
// pairs are treated as they are in With.
func (l clogger) Debugw(ctx context.Context, msg string, keysAndValues Fields) {
	if l.V(DebugLevel) {
		l.log.With(GetGlobalLFM().ExtractContextAttributes(ctx)...).Debugw(msg, serializeMap(keysAndValues)...)
	}
}

// Info logs a message at level Info on the standard logger.
func (l clogger) Info(ctx context.Context, args ...interface{}) {
	l.log.With(GetGlobalLFM().ExtractContextAttributes(ctx)...).Info(args...)
}

// Infoln logs a message at level Info on the standard logger with a line feed. Default in any case.
func (l clogger) Infoln(ctx context.Context, args ...interface{}) {
	l.log.With(GetGlobalLFM().ExtractContextAttributes(ctx)...).Info(args...)
	//msg := fmt.Sprintln(args...)
	//l.sourced().Info(msg[:len(msg)-1])
}

// Infof logs a message at level Info on the standard logger.
func (l clogger) Infof(ctx context.Context, format string, args ...interface{}) {
	l.log.With(GetGlobalLFM().ExtractContextAttributes(ctx)...).Infof(format, args...)
}

// Infow logs a message with some additional context. The variadic key-value
// pairs are treated as they are in With.
func (l clogger) Infow(ctx context.Context, msg string, keysAndValues Fields) {
	if l.V(InfoLevel) {
		l.log.With(GetGlobalLFM().ExtractContextAttributes(ctx)...).Infow(msg, serializeMap(keysAndValues)...)
	}
}

// Warn logs a message at level Warn on the standard logger.
func (l clogger) Warn(ctx context.Context, args ...interface{}) {
	l.log.With(GetGlobalLFM().ExtractContextAttributes(ctx)...).Warn(args...)
}

// Warnln logs a message at level Warn on the standard logger with a line feed. Default in any case.
func (l clogger) Warnln(ctx context.Context, args ...interface{}) {
	l.log.With(GetGlobalLFM().ExtractContextAttributes(ctx)...).Warn(args...)
}

// Warnf logs a message at level Warn on the standard logger.
func (l clogger) Warnf(ctx context.Context, format string, args ...interface{}) {
	l.log.With(GetGlobalLFM().ExtractContextAttributes(ctx)...).Warnf(format, args...)
}

// Warnw logs a message with some additional context. The variadic key-value
// pairs are treated as they are in With.
func (l clogger) Warnw(ctx context.Context, msg string, keysAndValues Fields) {
	if l.V(WarnLevel) {
		l.log.With(GetGlobalLFM().ExtractContextAttributes(ctx)...).Warnw(msg, serializeMap(keysAndValues)...)
	}
}

// Error logs a message at level Error on the standard logger.
func (l clogger) Error(ctx context.Context, args ...interface{}) {
	l.log.With(GetGlobalLFM().ExtractContextAttributes(ctx)...).Error(args...)
}

// Errorln logs a message at level Error on the standard logger with a line feed. Default in any case.
func (l clogger) Errorln(ctx context.Context, args ...interface{}) {
	l.log.With(GetGlobalLFM().ExtractContextAttributes(ctx)...).Error(args...)
}

// Errorf logs a message at level Error on the standard logger.
func (l clogger) Errorf(ctx context.Context, format string, args ...interface{}) {
	l.log.With(GetGlobalLFM().ExtractContextAttributes(ctx)...).Errorf(format, args...)
}

// Errorw logs a message with some additional context. The variadic key-value
// pairs are treated as they are in With.
func (l clogger) Errorw(ctx context.Context, msg string, keysAndValues Fields) {
	if l.V(ErrorLevel) {
		l.log.With(GetGlobalLFM().ExtractContextAttributes(ctx)...).Errorw(msg, serializeMap(keysAndValues)...)
	}
}

// Fatal logs a message at level Fatal on the standard logger.
func (l clogger) Fatal(ctx context.Context, args ...interface{}) {
	l.log.With(GetGlobalLFM().ExtractContextAttributes(ctx)...).Fatal(args...)
}

// Fatalln logs a message at level Fatal on the standard logger with a line feed. Default in any case.
func (l clogger) Fatalln(ctx context.Context, args ...interface{}) {
	l.log.With(GetGlobalLFM().ExtractContextAttributes(ctx)...).Fatal(args...)
}

// Fatalf logs a message at level Fatal on the standard logger.
func (l clogger) Fatalf(ctx context.Context, format string, args ...interface{}) {
	l.log.With(GetGlobalLFM().ExtractContextAttributes(ctx)...).Fatalf(format, args...)
}

// Fatalw logs a message with some additional context. The variadic key-value
// pairs are treated as they are in With.
func (l clogger) Fatalw(ctx context.Context, msg string, keysAndValues Fields) {
	if l.V(FatalLevel) {
		l.log.With(GetGlobalLFM().ExtractContextAttributes(ctx)...).Fatalw(msg, serializeMap(keysAndValues)...)
	}
}

// Warning logs a message at level Warn on the standard logger.
func (l clogger) Warning(ctx context.Context, args ...interface{}) {
	l.log.With(GetGlobalLFM().ExtractContextAttributes(ctx)...).Warn(args...)
}

// Warningln logs a message at level Warn on the standard logger with a line feed. Default in any case.
func (l clogger) Warningln(ctx context.Context, args ...interface{}) {
	l.log.With(GetGlobalLFM().ExtractContextAttributes(ctx)...).Warn(args...)
}

// Warningf logs a message at level Warn on the standard logger.
func (l clogger) Warningf(ctx context.Context, format string, args ...interface{}) {
	l.log.With(GetGlobalLFM().ExtractContextAttributes(ctx)...).Warnf(format, args...)
}

// V reports whether verbosity level l is at least the requested verbose level.
func (l clogger) V(level LogLevel) bool {
	return l.parent.Core().Enabled(logLevelToLevel(level))
}

// GetLogLevel returns the current level of the logger
func (l clogger) GetLogLevel() LogLevel {
	return levelToLogLevel(cfgs[l.packageName].Level.Level())
}
