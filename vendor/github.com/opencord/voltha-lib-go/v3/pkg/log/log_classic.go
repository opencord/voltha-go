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

// Older Version of Logger interface without support of Context Injection
// This is Depreciated and should not be used anymore. Instead use CLogger
// defined in log.go file.
// This file will be deleted once all code files of voltha compopnents have been
// changed to use new CLogger interface methods supporting context injection
package log

import (
	zp "go.uber.org/zap"
)

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
	V(l LogLevel) bool

	//Returns the log level of this specific logger
	GetLogLevel() LogLevel
}

// logger has been refactored to be a thin wrapper on clogger implementation to support
// all existing log statements during transition to new clogger
type logger struct {
	cl *clogger
}

func AddPackage(outputType string, level LogLevel, defaultFields Fields, pkgNames ...string) (Logger, error) {
	// Get package name of caller method and pass further on; else this method is considered caller
	pkgName, _, _, _ := getCallerInfo()

	pkgNames = append(pkgNames, pkgName)
	clg, err := RegisterPackage(outputType, level, defaultFields, pkgNames...)
	if err != nil {
		return nil, err
	}

	return logger{cl: clg.(*clogger)}, nil
}

func getPackageLevelSugaredLogger() *zp.SugaredLogger {
	pkgName, _, _, _ := getCallerInfo()
	if _, exist := loggers[pkgName]; exist {
		return loggers[pkgName].log
	}
	return defaultLogger.log
}

func getPackageLevelLogger() CLogger {
	pkgName, _, _, _ := getCallerInfo()
	if _, exist := loggers[pkgName]; exist {
		return loggers[pkgName]
	}
	return defaultLogger
}

// With returns a logger initialized with the key-value pairs
func (l logger) With(keysAndValues Fields) Logger {
	return logger{cl: &clogger{log: l.cl.log.With(serializeMap(keysAndValues)...), parent: l.cl.parent}}
}

// Debug logs a message at level Debug on the standard logger.
func (l logger) Debug(args ...interface{}) {
	l.cl.log.Debug(args...)
}

// Debugln logs a message at level Debug on the standard logger with a line feed. Default in any case.
func (l logger) Debugln(args ...interface{}) {
	l.cl.log.Debug(args...)
}

// Debugw logs a message at level Debug on the standard logger.
func (l logger) Debugf(format string, args ...interface{}) {
	l.cl.log.Debugf(format, args...)
}

// Debugw logs a message with some additional context. The variadic key-value
// pairs are treated as they are in With.
func (l logger) Debugw(msg string, keysAndValues Fields) {
	if l.V(DebugLevel) {
		l.cl.log.Debugw(msg, serializeMap(keysAndValues)...)
	}
}

// Info logs a message at level Info on the standard logger.
func (l logger) Info(args ...interface{}) {
	l.cl.log.Info(args...)
}

// Infoln logs a message at level Info on the standard logger with a line feed. Default in any case.
func (l logger) Infoln(args ...interface{}) {
	l.cl.log.Info(args...)
	//msg := fmt.Sprintln(args...)
	//l.sourced().Info(msg[:len(msg)-1])
}

// Infof logs a message at level Info on the standard logger.
func (l logger) Infof(format string, args ...interface{}) {
	l.cl.log.Infof(format, args...)
}

// Infow logs a message with some additional context. The variadic key-value
// pairs are treated as they are in With.
func (l logger) Infow(msg string, keysAndValues Fields) {
	if l.V(InfoLevel) {
		l.cl.log.Infow(msg, serializeMap(keysAndValues)...)
	}
}

// Warn logs a message at level Warn on the standard logger.
func (l logger) Warn(args ...interface{}) {
	l.cl.log.Warn(args...)
}

// Warnln logs a message at level Warn on the standard logger with a line feed. Default in any case.
func (l logger) Warnln(args ...interface{}) {
	l.cl.log.Warn(args...)
}

// Warnf logs a message at level Warn on the standard logger.
func (l logger) Warnf(format string, args ...interface{}) {
	l.cl.log.Warnf(format, args...)
}

// Warnw logs a message with some additional context. The variadic key-value
// pairs are treated as they are in With.
func (l logger) Warnw(msg string, keysAndValues Fields) {
	if l.V(WarnLevel) {
		l.cl.log.Warnw(msg, serializeMap(keysAndValues)...)
	}
}

// Error logs a message at level Error on the standard logger.
func (l logger) Error(args ...interface{}) {
	l.cl.log.Error(args...)
}

// Errorln logs a message at level Error on the standard logger with a line feed. Default in any case.
func (l logger) Errorln(args ...interface{}) {
	l.cl.log.Error(args...)
}

// Errorf logs a message at level Error on the standard logger.
func (l logger) Errorf(format string, args ...interface{}) {
	l.cl.log.Errorf(format, args...)
}

// Errorw logs a message with some additional context. The variadic key-value
// pairs are treated as they are in With.
func (l logger) Errorw(msg string, keysAndValues Fields) {
	if l.V(ErrorLevel) {
		l.cl.log.Errorw(msg, serializeMap(keysAndValues)...)
	}
}

// Fatal logs a message at level Fatal on the standard logger.
func (l logger) Fatal(args ...interface{}) {
	l.cl.log.Fatal(args...)
}

// Fatalln logs a message at level Fatal on the standard logger with a line feed. Default in any case.
func (l logger) Fatalln(args ...interface{}) {
	l.cl.log.Fatal(args...)
}

// Fatalf logs a message at level Fatal on the standard logger.
func (l logger) Fatalf(format string, args ...interface{}) {
	l.cl.log.Fatalf(format, args...)
}

// Fatalw logs a message with some additional context. The variadic key-value
// pairs are treated as they are in With.
func (l logger) Fatalw(msg string, keysAndValues Fields) {
	if l.V(FatalLevel) {
		l.cl.log.Fatalw(msg, serializeMap(keysAndValues)...)
	}
}

// Warning logs a message at level Warn on the standard logger.
func (l logger) Warning(args ...interface{}) {
	l.cl.log.Warn(args...)
}

// Warningln logs a message at level Warn on the standard logger with a line feed. Default in any case.
func (l logger) Warningln(args ...interface{}) {
	l.cl.log.Warn(args...)
}

// Warningf logs a message at level Warn on the standard logger.
func (l logger) Warningf(format string, args ...interface{}) {
	l.cl.log.Warnf(format, args...)
}

// V reports whether verbosity level l is at least the requested verbose level.
func (l logger) V(level LogLevel) bool {
	return l.cl.parent.Core().Enabled(logLevelToLevel(level))
}

// GetLogLevel returns the current level of the logger
func (l logger) GetLogLevel() LogLevel {
	return levelToLogLevel(cfgs[l.cl.packageName].Level.Level())
}

// With returns a logger initialized with the key-value pairs
func With(keysAndValues Fields) Logger {
	return logger{cl: &clogger{log: getPackageLevelSugaredLogger().With(serializeMap(keysAndValues)...), parent: defaultLogger.parent}}
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
func V(level LogLevel) bool {
	return getPackageLevelLogger().V(level)
}

//GetLogLevel returns the log level of the invoking package
func GetLogLevel() LogLevel {
	return getPackageLevelLogger().GetLogLevel()
}
