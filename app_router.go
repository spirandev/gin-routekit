package routekit

import (
	"fmt"
	"reflect"

	"github.com/gin-gonic/gin"
)

type RouteSyncer interface {
	SyncRoutes(routeIDs []int32, routes []Route) error
}

type AppRouter struct {
	registrars []RouteRegistrar
	syncer     RouteSyncer
}

func NewAppRouter(registrars any, syncer RouteSyncer) *AppRouter {
	return NewAppRouterFromRegistrars(collectRegistrars(registrars), syncer)
}

func NewAppRouterFromRegistrars(registrars []RouteRegistrar, syncer RouteSyncer) *AppRouter {
	return &AppRouter{
		registrars: registrars,
		syncer:     syncer,
	}
}

func (ar *AppRouter) RegisterRoutes(engine *gin.Engine) error {
	routes := []Route{}
	routeIDs := []int32{}

	for _, registrar := range ar.registrars {
		routeMeta := registrar.Register(engine)
		routes = append(routes, routeMeta)

		for _, handler := range routeMeta.Handlers {
			routeIDs = append(routeIDs, handler.RouteId)
		}
	}

	if ar.syncer == nil {
		return nil
	}
	if err := ar.syncer.SyncRoutes(routeIDs, routes); err != nil {
		return fmt.Errorf("sync routes: %w", err)
	}
	return nil
}

func collectRegistrars(registrars any) []RouteRegistrar {
	if registrars == nil {
		return nil
	}

	switch value := registrars.(type) {
	case []RouteRegistrar:
		return value
	case RouteRegistrar:
		return []RouteRegistrar{value}
	}

	val := reflect.ValueOf(registrars)
	for val.Kind() == reflect.Pointer {
		if val.IsNil() {
			return nil
		}
		val = val.Elem()
	}
	if val.Kind() != reflect.Struct {
		return nil
	}

	result := make([]RouteRegistrar, 0, val.NumField())
	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		if !field.CanInterface() || isNilValue(field) {
			continue
		}
		if registrar, ok := field.Interface().(RouteRegistrar); ok {
			result = append(result, registrar)
		}
	}
	return result
}

func isNilValue(value reflect.Value) bool {
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
