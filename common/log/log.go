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
package log

import (
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
	// PanicLevel logs a message, then panics.
	PanicLevel
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
}

// Fields is used as key-value pairs for structural logging
type Fields map[string]interface{}

var defaultLogger *logger
var cfg zp.Config

var loggers map[string]*logger
var cfgs map[string]zp.Config

type logger struct {
	log    *zp.SugaredLogger
	parent *zp.Logger
}

func parseLevel(l int) zp.AtomicLevel {
	switch l {
	case DebugLevel:
		return zp.NewAtomicLevelAt(zc.DebugLevel)
	case InfoLevel:
		return zp.NewAtomicLevelAt(zc.InfoLevel)
	case WarnLevel:
		return zp.NewAtomicLevelAt(zc.WarnLevel)
	case ErrorLevel:
		return zp.NewAtomicLevelAt(zc.ErrorLevel)
	case PanicLevel:
		return zp.NewAtomicLevelAt(zc.PanicLevel)
	case FatalLevel:
		return zp.NewAtomicLevelAt(zc.FatalLevel)
	}
	return zp.NewAtomicLevelAt(zc.ErrorLevel)
}

func getDefaultConfig(outputType string, level int, defaultFields Fields) zp.Config {
	return zp.Config{
		Level:            parseLevel(level),
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

func SetPackageLevelLoggers(outputType string, level int, defaultFields Fields, pkgNames []string) error {
	cfgs = make(map[string]zp.Config)
	loggers = make(map[string]*logger)
	for _, pkg := range pkgNames {
		// Build a custom config using zap - for now initialzie all packages uses the same config
		cfgs[pkg] = getDefaultConfig(outputType, level, defaultFields)

		l, err := cfgs[pkg].Build()
		if err != nil {
			return err
		}

		loggers[pkg] = &logger{
			log:    l.Sugar(),
			parent: l,
		}
	}

	return nil
}

func AddPackage(outputType string, level int, defaultFields Fields) error {
	if cfgs == nil {
		cfgs = make(map[string]zp.Config)
	}
	if loggers == nil {
		loggers = make(map[string]*logger)
	}
	pkgName, _, _, _ := getCallerInfo()

	if _, exist := cfgs[pkgName]; exist {
		return nil
	}
	cfgs[pkgName] = getDefaultConfig(outputType, level, defaultFields)

	l, err := cfgs[pkgName].Build()
	if err != nil {
		return err
	}

	loggers[pkgName] = &logger{
		log:    l.Sugar(),
		parent: l,
	}
	return nil
}

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
			log:    l.Sugar(),
			parent: l,
		}
	}
	return nil
}

//func SetDefaultLoglevel(level int) {
//	switch level {
//	case DebugLevel:
//		cfg.Level.SetLevel(zc.DebugLevel)
//	case InfoLevel:
//		cfg.Level.SetLevel(zc.InfoLevel)
//	case WarnLevel:
//		cfg.Level.SetLevel(zc.WarnLevel)
//	case ErrorLevel:
//		cfg.Level.SetLevel(zc.ErrorLevel)
//	case PanicLevel:
//		cfg.Level.SetLevel(zc.PanicLevel)
//	case FatalLevel:
//		cfg.Level.SetLevel(zc.FatalLevel)
//	default:
//		cfg.Level.SetLevel(zc.ErrorLevel)
//	}
//}

func SetPackageLogLevel(packageName string, level int) {
	// Get proper config
	if cfg, ok := cfgs[packageName]; ok {
		switch level {
		case DebugLevel:
			cfg.Level.SetLevel(zc.DebugLevel)
		case InfoLevel:
			cfg.Level.SetLevel(zc.InfoLevel)
		case WarnLevel:
			cfg.Level.SetLevel(zc.WarnLevel)
		case ErrorLevel:
			cfg.Level.SetLevel(zc.ErrorLevel)
		case PanicLevel:
			cfg.Level.SetLevel(zc.PanicLevel)
		case FatalLevel:
			cfg.Level.SetLevel(zc.FatalLevel)
		default:
			cfg.Level.SetLevel(zc.ErrorLevel)
		}
	}
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

// GetLogger returned the default logger.  If SetLogger was not previously invoked then
// this method will return an error
//func GetLogger() (Logger, error) {
//	if defaultLogger == nil {
//		// Setup the logger with default values - debug level,
//		SetDefaultLogger(JSON, 0, Fields{"instanceId": "default-logger"})
//		//return nil, errors.New("Uninitialized-logger")
//	}
//	return defaultLogger, nil
//}

//func extractFileNameAndLineNumber(skipLevel int) (string, int) {
//	_, file, line, ok := runtime.Caller(skipLevel)
//	var key string
//	if !ok {
//		key = "<???>"
//		line = 1
//	} else {
//		slash := strings.LastIndex(file, "/")
//		key = file[slash+1:]
//	}
//	return key, line
//}

// sourced adds a source field to the logger that contains
// the file name and line where the logging happened.
//func (l *logger) sourced() *zp.SugaredLogger {
//	key, line := extractFileNameAndLineNumber(3)
//	if strings.HasSuffix(key, "log.go") || strings.HasSuffix(key, "proc.go") {
//		// Go to a lower level
//		key, line = extractFileNameAndLineNumber(2)
//	}
//	if !strings.HasSuffix(key, ".go") {
//		// Go to a higher level
//		key, line = extractFileNameAndLineNumber(4)
//	}
//
//	return l.log.With("caller", fmt.Sprintf("%s:%d", key, line))
//}

func retrieveCallInfo(skiplevel int) (string, string, string, int) {
	pc, file, line, _ := runtime.Caller(skiplevel)
	_, fileName := path.Split(file)
	parts := strings.Split(runtime.FuncForPC(pc).Name(), ".")
	pl := len(parts)
	packageName := ""
	funcName := parts[pl-1]

	if parts[pl-2][0] == '(' {
		//funcName = parts[pl-2] + "." + funcName
		packageName = strings.Join(parts[0:pl-2], ".")
	} else {
		packageName = strings.Join(parts[0:pl-1], ".")
	}

	return packageName, fileName, funcName, line
}

func getCallerInfo() (string, string, string, int) {
	packageName, fileName, funcName, line := retrieveCallInfo(3)

	if strings.HasSuffix(funcName, "log.go") || strings.HasSuffix(funcName, "proc.go") || strings.HasSuffix(packageName, ".init") {
		// Go to a lower level
		packageName, fileName, funcName, line = retrieveCallInfo(2)
	}
	if !strings.HasSuffix(funcName, ".go") {
		// Go to a higher level
		packageName, fileName, funcName, line = retrieveCallInfo(4)
	}

	if strings.HasSuffix(fileName, ".go") {
		fileName = strings.TrimSuffix(fileName, ".go")
	}
	return packageName, fileName, funcName, line
}

func getPackageLevelLogger() *zp.SugaredLogger {
	pkgName, fileName, funcName, line := getCallerInfo()
	if _, exist := loggers[pkgName]; exist {
		return loggers[pkgName].log.With("caller", fmt.Sprintf("%s.%s:%d", fileName, funcName, line))
	}
	return defaultLogger.log.With("caller", fmt.Sprintf("%s.%s:%d", fileName, funcName, line))
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

// With returns a logger initialized with the key-value pairs
func With(keysAndValues Fields) Logger {
	return logger{log: getPackageLevelLogger().With(serializeMap(keysAndValues)...), parent: defaultLogger.parent}
}

// Debug logs a message at level Debug on the standard logger.
func Debug(args ...interface{}) {
	getPackageLevelLogger().Debug(args...)
}

// Debugln logs a message at level Debug on the standard logger.
func Debugln(args ...interface{}) {
	getPackageLevelLogger().Debug(args...)
}

// Debugf logs a message at level Debug on the standard logger.
func Debugf(format string, args ...interface{}) {
	getPackageLevelLogger().Debugf(format, args...)
}

// Debugw logs a message with some additional context. The variadic key-value
// pairs are treated as they are in With.
func Debugw(msg string, keysAndValues Fields) {
	getPackageLevelLogger().Debugw(msg, serializeMap(keysAndValues)...)
}

// Info logs a message at level Info on the standard logger.
func Info(args ...interface{}) {
	getPackageLevelLogger().Info(args...)
}

// Infoln logs a message at level Info on the standard logger.
func Infoln(args ...interface{}) {
	getPackageLevelLogger().Info(args...)
}

// Infof logs a message at level Info on the standard logger.
func Infof(format string, args ...interface{}) {
	getPackageLevelLogger().Infof(format, args...)
}

//Infow logs a message with some additional context. The variadic key-value
//pairs are treated as they are in With.
func Infow(msg string, keysAndValues Fields) {
	getPackageLevelLogger().Infow(msg, serializeMap(keysAndValues)...)
}

// Warn logs a message at level Warn on the standard logger.
func Warn(args ...interface{}) {
	getPackageLevelLogger().Warn(args...)
}

// Warnln logs a message at level Warn on the standard logger.
func Warnln(args ...interface{}) {
	getPackageLevelLogger().Warn(args...)
}

// Warnf logs a message at level Warn on the standard logger.
func Warnf(format string, args ...interface{}) {
	getPackageLevelLogger().Warnf(format, args...)
}

// Warnw logs a message with some additional context. The variadic key-value
// pairs are treated as they are in With.
func Warnw(msg string, keysAndValues Fields) {
	getPackageLevelLogger().Warnw(msg, serializeMap(keysAndValues)...)
}

// Error logs a message at level Error on the standard logger.
func Error(args ...interface{}) {
	getPackageLevelLogger().Error(args...)
}

// Errorln logs a message at level Error on the standard logger.
func Errorln(args ...interface{}) {
	getPackageLevelLogger().Error(args...)
}

// Errorf logs a message at level Error on the standard logger.
func Errorf(format string, args ...interface{}) {
	getPackageLevelLogger().Errorf(format, args...)
}

// Errorw logs a message with some additional context. The variadic key-value
// pairs are treated as they are in With.
func Errorw(msg string, keysAndValues Fields) {
	getPackageLevelLogger().Errorw(msg, serializeMap(keysAndValues)...)
}

// Fatal logs a message at level Fatal on the standard logger.
func Fatal(args ...interface{}) {
	getPackageLevelLogger().Fatal(args...)
}

// Fatalln logs a message at level Fatal on the standard logger.
func Fatalln(args ...interface{}) {
	getPackageLevelLogger().Fatal(args...)
}

// Fatalf logs a message at level Fatal on the standard logger.
func Fatalf(format string, args ...interface{}) {
	getPackageLevelLogger().Fatalf(format, args...)
}

// Fatalw logs a message with some additional context. The variadic key-value
// pairs are treated as they are in With.
func Fatalw(msg string, keysAndValues Fields) {
	getPackageLevelLogger().Fatalw(msg, serializeMap(keysAndValues)...)
}
