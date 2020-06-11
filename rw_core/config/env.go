package config

import (
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

func isTrue(s string) bool {
	b, _ := strconv.ParseBool(s)
	return b
}

var gatherRegexp = regexp.MustCompile("([^A-Z]+|[A-Z]+[^A-Z]+|[A-Z]+)")
var acronymRegexp = regexp.MustCompile("([A-Z]+)([A-Z][^A-Z]+)")

func BuildFlagSet(cmd string, spec interface{}) *pflag.FlagSet {
	s := reflect.ValueOf(spec)

	if s.Kind() != reflect.Ptr {
		return nil
	}
	s = s.Elem()
	if s.Kind() != reflect.Struct {
		return nil
	}

	set := pflag.NewFlagSet(cmd, pflag.ExitOnError)
	typeOfSpec := s.Type()
	for i := 0; i < s.NumField(); i++ {
		f := s.Field(i)
		ftype := typeOfSpec.Field(i)
		if !f.CanSet() || isTrue(ftype.Tag.Get("ignored")) {
			continue
		}

		key := strings.ToUpper(ftype.Tag.Get("env"))
		if len(key) == 0 {
			words := gatherRegexp.FindAllStringSubmatch(ftype.Name, -1)
			if len(words) == 0 {
				key = strings.ToUpper(ftype.Name)
			} else {
				var name []string
				for _, words := range words {
					if m := acronymRegexp.FindStringSubmatch(words[0]); len(m) == 3 {
						name = append(name, m[1], m[2])
					} else {
						name = append(name, words[0])
					}
				}

				key = strings.ToUpper(strings.Join(name, "_"))
			}
		}

		def := ftype.Tag.Get("default")
		long := ftype.Tag.Get("long")
		if len(long) == 0 {
			long = strings.ToLower(key)
		}
		switch f.Type().Kind() {
		case reflect.String:
			set.String(long, def, ftype.Tag.Get("desc"))
			viper.SetDefault(ftype.Name, def)
			_ = viper.BindPFlag(ftype.Name, set.Lookup(long))
			_ = viper.BindEnv(ftype.Name, key)
		case reflect.Bool:
			val, _ := strconv.ParseBool(def)
			set.Bool(long, val, ftype.Tag.Get("desc"))
			viper.SetDefault(ftype.Name, def)
			_ = viper.BindPFlag(ftype.Name, set.Lookup(long))
			_ = viper.BindEnv(ftype.Name, key)
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			if f.Type().Kind() == reflect.Int64 && f.Type().PkgPath() == "time" && f.Type().Name() == "Duration" {
				val, _ := time.ParseDuration(def)
				set.Duration(long, val, ftype.Tag.Get("desc"))
				viper.SetDefault(ftype.Name, def)
				_ = viper.BindPFlag(ftype.Name, set.Lookup(long))
				_ = viper.BindEnv(ftype.Name, key)
			} else {
				val, _ := strconv.ParseInt(def, 0, f.Type().Bits())
				set.Int(long, int(val), ftype.Tag.Get("desc"))
				viper.SetDefault(ftype.Name, def)
				_ = viper.BindPFlag(ftype.Name, set.Lookup(long))
				_ = viper.BindEnv(ftype.Name, key)
			}
		}
	}

	return set
}
