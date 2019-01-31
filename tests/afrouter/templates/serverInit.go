
package main

var servers map[string]*serverCtl

func serverInit() error {
	servers = make(map[string]*serverCtl)
	{{range .}}
	if err := {{.Name}}ListenAndServe(); err != nil {
		return err
	}
	{{end}}
	return nil
}


