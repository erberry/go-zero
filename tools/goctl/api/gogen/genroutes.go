package gogen

import (
	"bytes"
	"fmt"
	"path"
	"sort"
	"strings"
	"text/template"

	"zero/core/collection"
	"zero/tools/goctl/api/spec"
	apiutil "zero/tools/goctl/api/util"
	"zero/tools/goctl/util"
	"zero/tools/goctl/vars"
)

const (
	routesFilename = "routes.go"
	routesTemplate = `// DO NOT EDIT, generated by goctl
package handler

import (
	"net/http"

	{{.importPackages}}
)

func RegisterHandlers(engine *rest.Server, serverCtx *svc.ServiceContext) {
	{{.routesAdditions}}
}
`
	routesAdditionTemplate = `
	engine.AddRoutes([]rest.Route{
		{{.routes}}
	}{{.jwt}}{{.signature}})
`
)

var mapping = map[string]string{
	"delete": "http.MethodDelete",
	"get":    "http.MethodGet",
	"head":   "http.MethodHead",
	"post":   "http.MethodPost",
	"put":    "http.MethodPut",
}

type (
	group struct {
		routes           []route
		jwtEnabled       bool
		signatureEnabled bool
		authName         string
	}
	route struct {
		method  string
		path    string
		handler string
	}
)

func genRoutes(dir string, api *spec.ApiSpec) error {
	var builder strings.Builder
	groups, err := getRoutes(api)
	if err != nil {
		return err
	}

	gt := template.Must(template.New("groupTemplate").Parse(routesAdditionTemplate))
	for _, g := range groups {
		var gbuilder strings.Builder
		for _, r := range g.routes {
			fmt.Fprintf(&gbuilder, `
		{
			Method:  %s,
			Path:    "%s",
			Handler: %s,
		},`,
				r.method, r.path, r.handler)
		}
		var jwt string
		if g.jwtEnabled {
			jwt = fmt.Sprintf(", ngin.WithJwt(serverCtx.Config.%s.AccessSecret)", g.authName)
		}
		var signature string
		if g.signatureEnabled {
			signature = fmt.Sprintf(", ngin.WithSignature(serverCtx.Config.%s.Signature)", g.authName)
		}
		if err := gt.Execute(&builder, map[string]string{
			"routes":    strings.TrimSpace(gbuilder.String()),
			"jwt":       jwt,
			"signature": signature,
		}); err != nil {
			return err
		}
	}

	parentPkg, err := getParentPackage(dir)
	if err != nil {
		return err
	}

	filename := path.Join(dir, handlerDir, routesFilename)
	if err := util.RemoveOrQuit(filename); err != nil {
		return err
	}

	fp, created, err := apiutil.MaybeCreateFile(dir, handlerDir, routesFilename)
	if err != nil {
		return err
	}
	if !created {
		return nil
	}
	defer fp.Close()

	t := template.Must(template.New("routesTemplate").Parse(routesTemplate))
	buffer := new(bytes.Buffer)
	err = t.Execute(buffer, map[string]string{
		"importPackages":  genRouteImports(parentPkg, api),
		"routesAdditions": strings.TrimSpace(builder.String()),
	})
	if err != nil {
		return nil
	}
	formatCode := formatCode(buffer.String())
	_, err = fp.WriteString(formatCode)
	return err
}

func genRouteImports(parentPkg string, api *spec.ApiSpec) string {
	var importSet = collection.NewSet()
	importSet.AddStr(fmt.Sprintf("\"%s/rest\"", vars.ProjectOpenSourceUrl))
	importSet.AddStr(fmt.Sprintf("\"%s\"", path.Join(parentPkg, contextDir)))
	for _, group := range api.Service.Groups {
		for _, route := range group.Routes {
			folder, ok := apiutil.GetAnnotationValue(route.Annotations, "server", folderProperty)
			if !ok {
				folder, ok = apiutil.GetAnnotationValue(group.Annotations, "server", folderProperty)
				if !ok {
					continue
				}
			}
			importSet.AddStr(fmt.Sprintf("%s \"%s\"", folder, path.Join(parentPkg, handlerDir, folder)))
		}
	}
	imports := importSet.KeysStr()
	sort.Strings(imports)
	return strings.Join(imports, "\n\t")
}

func getRoutes(api *spec.ApiSpec) ([]group, error) {
	var routes []group

	for _, g := range api.Service.Groups {
		var groupedRoutes group
		for _, r := range g.Routes {
			handler, ok := apiutil.GetAnnotationValue(r.Annotations, "server", "handler")
			if !ok {
				return nil, fmt.Errorf("missing handler annotation for route %q", r.Path)
			}
			handler = getHandlerBaseName(handler) + "Handler(serverCtx)"
			folder, ok := apiutil.GetAnnotationValue(r.Annotations, "server", folderProperty)
			if ok {
				handler = folder + "." + strings.ToUpper(handler[:1]) + handler[1:]
			} else {
				folder, ok = apiutil.GetAnnotationValue(g.Annotations, "server", folderProperty)
				if ok {
					handler = folder + "." + strings.ToUpper(handler[:1]) + handler[1:]
				}
			}
			groupedRoutes.routes = append(groupedRoutes.routes, route{
				method:  mapping[r.Method],
				path:    r.Path,
				handler: handler,
			})
		}
		routes = append(routes, groupedRoutes)
	}

	return routes, nil
}
