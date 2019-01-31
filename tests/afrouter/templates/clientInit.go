
package main

var clients map[string]*clientCtl

func clientInit() error {
	clients = make(map[string]*clientCtl)
	{{range .}}
	if _,err := {{.Name}}Connect(); err != nil {
		return err
	}
	{{end}}
	return nil
}


