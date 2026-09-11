# Guia completo do gin-routekit

Este guia mostra como usar toda a biblioteca `gin-routekit`: grupos e rotas Gin, middlewares, metadados de autenticacao, registro centralizado, sincronizacao, documentacao OpenAPI 3.1, Swagger UI, contracts, schemas direcionais e endpoints JSON tipados.

## Sumario

1. [Instalacao](#instalacao)
2. [Como a biblioteca funciona](#como-a-biblioteca-funciona)
3. [Exemplo completo](#exemplo-completo)
4. [Grupos e rotas](#grupos-e-rotas)
5. [Middlewares e contexto](#middlewares-e-contexto)
6. [Flags de autenticacao e metadados](#flags-de-autenticacao-e-metadados)
7. [RouteRegistrar e AppRouter](#routeregistrar-e-approuter)
8. [Documentacao de endpoints](#documentacao-de-endpoints)
9. [Contracts e defaults](#contracts-e-defaults)
10. [Profiles e middleware documentado](#profiles-e-middleware-documentado)
11. [OpenAPI 3.1](#openapi-31)
12. [Schemas, registrations e providers](#schemas-registrations-e-providers)
13. [Security schemes e regras](#security-schemes-e-regras)
14. [Decorators](#decorators)
15. [Swagger UI](#swagger-ui)
16. [jsonendpoint](#jsonendpoint)
17. [Validacao, testes e CI](#validacao-testes-e-ci)
18. [Erros comuns](#erros-comuns)
19. [Referencia rapida](#referencia-rapida)

## Instalacao

O modulo e:

```text
github.com/spirandev/gin-routekit
```

Instale com:

Instale uma release ou commit que contenha as APIs OpenAPI 3.1 descritas neste guia:

```bash
go get github.com/spirandev/gin-routekit@<versao-ou-commit>
```

Imports usuais:

```go
import (
	"github.com/gin-gonic/gin"
	routekit "github.com/spirandev/gin-routekit"
	"github.com/spirandev/gin-routekit/jsonendpoint"
)
```

A versao atual do modulo usa Go 1.25 e Gin 1.12.

## Como a biblioteca funciona

O fluxo principal possui quatro etapas:

1. Um `RouteRegistrar` cria um `RouterGroup`, define endpoints e devolve `group.Export(...)`.
2. `AppRouter.RegisterRoutes(engine)` chama `RouteRegistrar.Register(engine)` em cada registrar.
3. O `AppRouter` guarda um snapshot defensivo das rotas e, opcionalmente, chama um `RouteSyncer`.
4. O mesmo snapshot pode ser validado, convertido em OpenAPI 3.1 e publicado por HTTP.

Definir uma rota com `group.GET`, `group.POST` ou outro metodo apenas adiciona a definicao ao grupo. O registro real no Gin acontece quando `RouterGroup.Export` e executado pelo registrar.

```text
RouterGroup.GET/POST/...
        |
        v
RouterGroup.Export
        |
        +--> registra handlers no Gin
        +--> devolve Route
                   |
                   v
           AppRouter snapshot
                   |
                   +--> RouteSyncer
                   +--> ValidateOpenAPI
                   +--> BuildOpenAPI
                   +--> RegisterOpenAPI
```

## Exemplo completo

O exemplo abaixo registra uma rota publica, uma rota autenticada, OpenAPI e Swagger UI.

### DTOs

```go
package api

type CreateUserRequest struct {
	Name  string `json:"name" binding:"required"`
	Email string `json:"email" binding:"required,email"`
}

type UserResponse struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}
```

### Registrar de rotas

```go
package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	routekit "github.com/spirandev/gin-routekit"
)

type UserRoutes struct {
	AuthFactory   func() gin.HandlerFunc
	Authorization gin.HandlerFunc
}

func (routes UserRoutes) Register(engine *gin.Engine) routekit.Route {
	group := routekit.NewRouterGroup(
		engine,
		"/api",
		routekit.WithAuthMiddlewareFactory(routes.AuthFactory),
		routekit.WithAuthorizationMiddleware(routes.Authorization),
		routekit.WithDefaultResponse(
			http.StatusInternalServerError,
			"Internal Server Error",
			routekit.SchemaOf[ErrorResponse](),
		),
	)

	group.GET("/health", health, "Health check", 100).
		Public().
		Contract(routekit.EmptyJSONResponseContract(http.StatusNoContent, "No Content"))

	group.POST("/users", createUser, "Create user", 101).
		Tags("users").
		Scopes("users:write").
		Contract(
			routekit.JSONRequestContractOf[CreateUserRequest, UserResponse](
				http.StatusCreated,
				"Created",
				routekit.WithAdditionalResponse(
					routekit.ResponseOf[ErrorResponse](http.StatusBadRequest, "Bad Request"),
				),
			),
		)

	group.GET("/users/:id", getUser, "Get user", 102).
		PathParam("id", "string", true, "User ID").
		Contract(routekit.JSONResponseContractOf[UserResponse](http.StatusOK, "OK"))

	return group.Export("users", 10)
}
```

Todos os placeholders Gin precisam de um parametro OpenAPI correspondente. Por isso `/users/:id` declara `PathParam("id", ...)`.

### Inicializacao da aplicacao

```go
package main

import (
	"log"

	"github.com/gin-gonic/gin"
	routekit "github.com/spirandev/gin-routekit"
	"meu-modulo/api"
)

func main() {
	engine := gin.New()
	engine.Use(gin.Recovery())

	registrars := []routekit.RouteRegistrar{
		api.UserRoutes{
			AuthFactory:   newAuthMiddleware,
			Authorization: authorizationMiddleware,
		},
	}

	appRouter := routekit.NewAppRouterFromRegistrars(registrars, nil)
	if err := appRouter.RegisterRoutes(engine); err != nil {
		log.Fatal(err)
	}

	openAPIConfig := routekit.OpenAPIConfig{
		Title:             "Users API",
		Version:           "1.0.0",
		Description:       "User management API",
		BasePath:          "/api",
		PathMode:          routekit.PathsRelativeToBase,
		JSONPath:          "/openapi.json",
		DocumentationMode: routekit.DocumentAll,
		Components: routekit.OpenAPIComponents{
			SecuritySchemes: map[string]*routekit.OpenAPISecurityScheme{
				"BearerAuth": routekit.BearerSecurityScheme("JWT access token"),
			},
		},
		SecurityRules: []routekit.RouteSecurityRule{
			routekit.WhenAuthenticated(
				routekit.RequireSecurityScheme("BearerAuth"),
			),
		},
	}

	report, err := appRouter.ValidateOpenAPI(openAPIConfig)
	for _, diagnostic := range report.Diagnostics {
		log.Printf(
			"openapi severity=%s code=%s method=%s path=%s message=%s",
			diagnostic.Severity,
			diagnostic.Code,
			diagnostic.Method,
			diagnostic.Path,
			diagnostic.Message,
		)
	}
	if err != nil {
		log.Fatal(err)
	}

	if err := appRouter.RegisterOpenAPI(engine, openAPIConfig); err != nil {
		log.Fatal(err)
	}
	if err := appRouter.RegisterSwaggerUI(engine, routekit.SwaggerUIConfig{
		Path:       "/docs",
		OpenAPIURL: "/openapi.json",
		Title:      "Users API",
	}); err != nil {
		log.Fatal(err)
	}

	if err := engine.Run(":8080"); err != nil {
		log.Fatal(err)
	}
}
```

Resultado:

```text
API runtime:  http://localhost:8080/api/...
OpenAPI JSON: http://localhost:8080/openapi.json
Swagger UI:   http://localhost:8080/docs
```

## Grupos e rotas

### Criar um grupo

```go
group := routekit.NewRouterGroup(engine, "/api")
```

Opcoes runtime disponiveis:

```go
group := routekit.NewRouterGroup(
	engine,
	"/api",
	routekit.WithAuthMiddlewareFactory(authFactory),
	routekit.WithAuthorizationMiddleware(authorizationMiddleware),
	routekit.WithSameApplicationMiddleware(sameApplicationMiddleware),
	routekit.WithRouteContextKeys("route_id", "application_id"),
)
```

Opcoes documentais do grupo:

```go
group := routekit.NewRouterGroup(
	engine,
	"/api",
	routekit.WithDocumentation(),
	routekit.WithDocProfiles("tenant"),
	routekit.WithDefaultHeader(routekit.DocParam{
		Name:        "X-Tenant",
		Type:        "string",
		Required:    true,
		Description: "Tenant identifier",
	}),
	routekit.WithDefaultQuery(routekit.DocParam{
		Name: "locale",
		Type: "string",
	}),
	routekit.WithDefaultResponse(401, "Unauthorized", routekit.SchemaOf[ErrorResponse]()),
	routekit.WithDefaultResponseWith(
		429,
		"Too Many Requests",
		"application/problem+json",
		routekit.SchemaOf[ErrorResponse](),
	),
	routekit.WithDefaultContentTypes("application/json", "application/json"),
)
```

`WithDefaultHeader`, `WithDefaultQuery` e `WithDefaultPathParam` corrigem automaticamente o campo `In` do parametro.

### Metodos de rota

Atalhos disponiveis:

```go
group.GET(path, handler, description, routeID)
group.HEAD(path, handler, description, routeID)
group.POST(path, handler, description, routeID)
group.PUT(path, handler, description, routeID)
group.PATCH(path, handler, description, routeID)
group.DELETE(path, handler, description, routeID)
group.OPTIONS(path, handler, description, routeID)
```

Para outro metodo:

```go
group.Handle(http.MethodTrace, "/trace", handler, "Trace", 200)
```

`CONNECT` existe em `ValidMethods`, mas nao e representavel no `OpenAPIPathItem` e causa erro no build OpenAPI.

Cada metodo devolve `*RouteConfig`, permitindo encadear configuracoes:

```go
group.POST("/orders", createOrder, "Create order", 300).
	RequireClientContext().
	Scopes("orders:write").
	Tags("orders").
	Contract(routekit.JSONRequestContractOf[CreateOrderRequest, OrderResponse](201, "Created"))
```

### Exportar o grupo

```go
routeSnapshot := group.Export("orders", 20)
```

Argumentos:

```text
groupName: nome documental do grupo e tag default das operacoes
appID:     identificador da aplicacao colocado no gin.Context
```

`Export` registra as rotas no Gin. Nao execute `Export` duas vezes para o mesmo grupo e engine, pois o Gin rejeita rotas duplicadas.

## Middlewares e contexto

### Middleware de grupo

```go
group.Use(requestLogger, metricsMiddleware)
```

### Middleware de rota

```go
group.GET("/reports", reports, "Reports", 400).
	Use(rateLimitMiddleware)
```

### Ordem runtime

A cadeia adicionada pelo routekit a cada endpoint segue esta ordem:

```text
1. middleware interno de RouteID/ApplicationID
2. middlewares do grupo
3. middleware de autenticacao
4. middleware de mesma aplicacao
5. middleware de autorizacao
6. middlewares da rota
7. handler final
```

Os middlewares de autenticacao, mesma aplicacao e autorizacao dependem das flags da rota e das opcoes configuradas no grupo.

Middlewares globais instalados com `engine.Use(...)` executam antes dessa cadeia, conforme o comportamento do Gin.

### Ler RouteID e ApplicationID

Chaves default:

```text
RouteID
ApplicationID
```

Exemplo:

```go
func observabilityMiddleware(c *gin.Context) {
	routeID, _ := c.Get("RouteID")
	applicationID, _ := c.Get("ApplicationID")

	logger.Info("request", "route_id", routeID, "application_id", applicationID)
	c.Next()
}
```

Para usar outras chaves:

```go
routekit.WithRouteContextKeys("route_id", "application_id")
```

## Flags de autenticacao e metadados

Uma rota nova comeca autenticada, autorizada e restrita a mesma aplicacao.

```go
route.Public()
```

Desliga autenticacao e autorizacao.

```go
route.NoAuthz()
```

Mantem autenticacao e desliga apenas autorizacao.

```go
route.AllowAnySessionApp()
```

Desliga a exigencia de mesma aplicacao.

Outros metadados:

```go
route.RequireClientContext()
route.NoClientContext()
route.BasicRoute()
route.M2MRoute()
route.IntegrationRoute()
route.Scopes("read", "write")
```

Pontos importantes:

- `RequireClientContext`, `BasicRoute`, `M2MRoute`, `IntegrationRoute` e `Scopes` nao instalam middleware automaticamente.
- `M2MRoute` e `IntegrationRoute` podem ser usados por `SecurityRules` para gerar documentacao.
- `Scopes` substitui os scopes anteriores e copia o slice recebido.
- `NoAuthz` nao torna a rota publica.
- Quando autenticacao esta desligada, os middlewares de mesma aplicacao e autorizacao tambem nao executam.

## RouteRegistrar e AppRouter

### Implementar RouteRegistrar

```go
type ProductRoutes struct{}

func (ProductRoutes) Register(engine *gin.Engine) routekit.Route {
	group := routekit.NewRouterGroup(engine, "/api")
	group.GET("/products", listProducts, "List products", 500).
		Contract(routekit.JSONResponseContractOf[[]ProductResponse](200, "OK"))
	return group.Export("products", 30)
}
```

### Criar AppRouter

Forma explicita recomendada:

```go
appRouter := routekit.NewAppRouterFromRegistrars(
	[]routekit.RouteRegistrar{
		ProductRoutes{},
		UserRoutes{},
	},
	nil,
)
```

Tambem existe:

```go
appRouter := routekit.NewAppRouter(registrars, syncer)
```

`NewAppRouter` aceita um registrar, `[]RouteRegistrar` ou uma struct cujos campos implementem `RouteRegistrar`. Prefira `NewAppRouterFromRegistrars` para evitar conversoes silenciosas de tipos nao suportados.

### Registrar rotas

```go
if err := appRouter.RegisterRoutes(engine); err != nil {
	return err
}
```

`RegisterRoutes` nao e idempotente. Registrar novamente as mesmas rotas normalmente causa panic de rota duplicada no Gin.

### Consultar o snapshot

```go
routes := appRouter.Routes()
```

`Routes()` devolve uma copia defensiva da estrutura de rotas e dos metadados documentais conhecidos. Alterar os slices principais retornados nao altera o snapshot do `AppRouter`.

Valores arbitrarios guardados em `Example any` possuem clonagem profunda limitada. Maps e slices tipados ou structs com referencias internas podem continuar compartilhando dados; trate exemplos anexados como imutaveis.

### Sincronizar rotas

Implemente:

```go
type DatabaseRouteSyncer struct{}

func (DatabaseRouteSyncer) SyncRoutes(routeIDs []int32, routes []routekit.Route) error {
	// Persistir ou comparar metadados de rotas.
	return nil
}
```

Configure:

```go
appRouter := routekit.NewAppRouterFromRegistrars(registrars, DatabaseRouteSyncer{})
```

O syncer e executado depois do registro no Gin e depois do snapshot ser salvo. Se o sync falhar, nao existe rollback automatico das rotas.

## Documentacao de endpoints

### Habilitar ou ocultar

```go
route.Document()
route.HideFromDocs()
```

`HideFromDocs` e o override mais especifico.

### Metadados basicos

```go
route.
	Summary("Create user").
	Description("Creates one user in the current tenant").
	OperationID("createUser").
	Tags("users", "write")
```

Defaults:

```text
Summary:     descricao informada no registro da rota
Description: vazia
Tags:        Route.Group
OperationID: gerado automaticamente
```

### Parametros

```go
route.Header("X-Request-ID", "string", true, "Request identifier")
route.Query("page", "integer", false, "Page number")
route.PathParam("id", "string", true, "Resource ID")
```

Headers sao deduplicados sem diferenciar maiusculas e minusculas. Query e path diferenciam.

### Request body e responses

```go
route.Body(routekit.SchemaOf[CreateUserRequest]())

route.BodyWith(
	"Create user payload",
	true,
	routekit.SchemaOf[CreateUserRequest](),
)

route.Response(200, "OK", routekit.SchemaOf[UserResponse]())

route.ResponseWith(
	200,
	"OK",
	"application/json",
	routekit.SchemaOf[UserResponse](),
)
```

Use schema `nil` em uma response sem body:

```go
route.Response(204, "No Content", nil)
```

Um request body precisa de schema utilizavel. Status 204 e 205 nao podem ter schema de response.

### Remover valores herdados

```go
route.WithoutDocProfile("tenant")
route.WithoutDefaultResponse(401, 403)
route.WithoutDefaultParameter(routekit.DocParamInHeader, "X-Tenant")
```

Os tombstones removem valores herdados de defaults e middlewares. O contract e a configuracao do proprio endpoint ainda podem reintroduzir o valor.

## Contracts e defaults

Contracts evitam repetir `Body` e `Response` em endpoints JSON.

### Request e response

```go
contract := routekit.JSONRequestContractOf[CreateUserRequest, UserResponse](
	http.StatusCreated,
	"Created",
)
route.Contract(contract)
```

### Somente response

```go
route.Contract(
	routekit.JSONResponseContractOf[UserResponse](http.StatusOK, "OK"),
)
```

### Response sem body

```go
route.Contract(
	routekit.EmptyJSONResponseContract(http.StatusNoContent, "No Content"),
)
```

### Helper legado

```go
routekit.JSONContractOf[Request, Response](status, description)
```

Esse helper continua disponivel como alias deprecated de `JSONRequestContractOf`.

### Opcoes de contract

```go
routekit.WithOptionalRequestBody()
routekit.WithRequestContentType("application/json")
routekit.WithResponseContentType("application/json")
routekit.WithRequestExample(example)
routekit.WithResponseExample(example)
routekit.WithRequestExamples(routekit.NamedExample{Name: "whatsapp", Summary: "WhatsApp", Value: payload})
routekit.WithResponseExamples(routekit.NamedExample{Name: "success", Summary: "Delivered", Value: payload})
routekit.WithAdditionalResponse(response)
routekit.WithoutRequestBody()
routekit.WithoutResponseBody()
routekit.WithContractProfiles("tenant")
routekit.WithContractParameter(parameter)
```

Nem toda opcao e valida em todo constructor:

- `JSONResponseContractOf` rejeita opcoes especificas de request.
- `EmptyJSONResponseContract` rejeita opcoes especificas de request e opcoes de body da response.
- Incoerencias aparecem como diagnostico `contract.option.incoherent` no build OpenAPI.
- Combinar `WithRequestExample` com `WithRequestExamples` (ou o par de response) tambem e incoerencia: exemplos nomeados vencem, mas o diagnostico falha o build.
- Exemplos nomeados seguem o Example Object do OpenAPI: exatamente um de `Value` ou `ExternalValue`, nome nao vazio; violacoes geram `media_type.example.invalid`.

Exemplo:

```go
contract := routekit.JSONRequestContractOf[SearchRequest, SearchResponse](
	200,
	"OK",
	routekit.WithOptionalRequestBody(),
	routekit.WithRequestExample(SearchRequest{Query: "routekit"}),
	routekit.WithResponseExample(SearchResponse{Total: 1}),
	routekit.WithAdditionalResponse(
		routekit.ResponseOf[ErrorResponse](400, "Bad Request"),
	),
)
```

Exemplos nomeados sao serializados como o mapa `examples` do media type, entao o Swagger UI mostra um dropdown de cenarios no "Try it out" (ex.: "WhatsApp" / "E-mail"):

```go
contract := routekit.JSONRequestContractOf[NotifyRequest, NotifyResponse](
	200,
	"OK",
	routekit.WithRequestExamples(
		routekit.NamedExample{Name: "whatsapp", Summary: "WhatsApp", Value: NotifyRequest{Channel: "whatsapp"}},
		routekit.NamedExample{Name: "email", Summary: "E-mail", Value: NotifyRequest{Channel: "email"}},
	),
)
```

### ResponseOf

```go
response := routekit.ResponseOf[ErrorResponse](401, "Unauthorized")
```

Cria uma `DocResponse` com `application/json` e `SchemaOf[T]()`.

### Defaults globais JSON

```go
config.Defaults = routekit.WithJSONDefaults(
	routekit.DefaultResponseOf[ErrorResponse](500, "Internal Server Error"),
)
```

`WithJSONDefaults` devolve `DocumentationDefaults`; ele nao e um `GroupOption`.

### Precedencia documental

```text
defaults globais
< defaults do grupo
< middleware documentado do grupo
< middleware documentado da rota
< regras de seguranca
< Contract
< DocConfig e fluents
< decorators
```

Camadas declarativas mais especificas substituem parametros e responses da mesma chave. Security requirements sao acumulados e deduplicados.

Decorators executam por ultimo, mas os helpers `AddParameter`, `AddHeader`, `AddQuery`, `AddPathParam` e `AddResponse` nao sobrescrevem uma chave existente. Uma definicao conflitante mantem o valor anterior e gera diagnostico bloqueante.

## Profiles e middleware documentado

### Declarar profiles

```go
config.Profiles = map[string]routekit.DocProfile{
	"tenant": {
		Description: "Tenant context",
		Headers: []routekit.DocParam{{
			Name:        "X-Tenant",
			In:          routekit.DocParamInHeader,
			Type:        "string",
			Required:    true,
			Description: "Tenant identifier",
		}},
		Security: []string{"BearerAuth"},
	},
}
```

Aplicar um profile:

```go
route.DocProfile("tenant")
```

Outras formas:

```go
routekit.WithDocProfiles("tenant")
routekit.WithContractProfiles("tenant")
routekit.UsesProfiles("tenant")
```

Profiles efetivamente usados aparecem em `x-routekit-profiles` no documento OpenAPI.

### Middleware documentado do grupo

```go
group.UseDocumented(
	authMiddleware,
	routekit.RequireSecurityScheme("BearerAuth"),
	routekit.RespondsWith(
		routekit.ResponseOf[ErrorResponse](401, "Unauthorized"),
	),
)
```

### Middleware documentado da rota

```go
route.UseDocumented(
	rateLimitMiddleware,
	routekit.RequiresHeader(
		"X-Request-ID",
		"string",
		true,
		"Request identifier",
	),
	routekit.RespondsWith(
		routekit.ResponseOf[ErrorResponse](429, "Too Many Requests"),
	),
)
```

Contribuicoes disponiveis:

```go
routekit.RequireSecurityScheme("BearerAuth")
routekit.RequireOperationHeader(routekit.DocParam{...})
routekit.RequiresHeader("X-Request-ID", "Request identifier")
routekit.RequiresSecurity(requirementA, requirementB)
routekit.RespondsWith(responseA, responseB)
routekit.UsesProfiles("tenant")
```

`MiddlewareMetadata` continua aceito como contribuicao:

```go
group.UseDocumented(middleware, routekit.MiddlewareMetadata{
	Profiles: []string{"tenant"},
	Responses: []routekit.DocResponse{
		routekit.ResponseOf[ErrorResponse](401, "Unauthorized"),
	},
})
```

As contribuicoes nao inspecionam o codigo do middleware. Elas apenas declaram seu efeito documental.

## OpenAPI 3.1

### Configuracao minima

```go
config := routekit.OpenAPIConfig{
	Title:             "Example API",
	Version:           "1.0.0",
	BasePath:          "/api",
	PathMode:          routekit.PathsRelativeToBase,
	DocumentationMode: routekit.DocumentAll,
}
```

Campos obrigatorios:

```text
Title
Version
BasePath
PathMode
```

`BasePath` deve ser absoluto e normalizado, sem query, fragment ou trailing slash, exceto `/`.

### Modos documentais

```go
routekit.DocumentAll
routekit.DocumentOptIn
```

`DocumentAll` inclui todas as rotas, salvo `HideFromDocs`.

`DocumentOptIn` comeca desabilitado. Use `Document`, `WithDocumentation` ou defaults com `Enabled` para habilitar.

`EnabledByDefault` continua disponivel apenas para compatibilidade quando `DocumentationMode` esta vazio.

### Path relativo ao base path

```go
BasePath: "/api",
PathMode: routekit.PathsRelativeToBase,
```

Rota registrada:

```text
/api/users
```

Path OpenAPI:

```text
/users
```

Sem `Servers`, o builder gera:

```json
{"url":"/api"}
```

### Paths completos

```go
BasePath: "/api",
PathMode: routekit.FullRegisteredPaths,
Servers: []routekit.OpenAPIServer{
	{URL: "https://api.example.com"},
},
```

O path continua `/api/users`. Nesse modo, o `Server.URL` nao deve repetir `/api`.

### Validar

```go
report, err := routekit.ValidateOpenAPI(routes, config)
```

Ou com `AppRouter`:

```go
report, err := appRouter.ValidateOpenAPI(config)
```

Warnings nao bloqueiam o build. Erros devolvem `*DiagnosticsError` e o relatorio completo.

```go
if report.HasErrors() {
	log.Print("the OpenAPI report contains errors")
}
```

```go
var diagnosticsError *routekit.DiagnosticsError
if errors.As(err, &diagnosticsError) {
	for _, diagnostic := range diagnosticsError.Report.Diagnostics {
		log.Printf("%s: %s", diagnostic.Code, diagnostic.Message)
	}
}
```

Cada diagnostico possui:

```go
type Diagnostic struct {
	Code     string
	Severity DiagnosticSeverity
	Method   string
	Path     string
	Route    string
	Location string
	Message  string
}
```

### Build sem endpoint HTTP

```go
document, err := routekit.BuildOpenAPI(routes, config)
```

Com snapshot do router:

```go
document, err := appRouter.BuildOpenAPI(config)
```

### Marshal deterministico

```go
payload, err := routekit.MarshalOpenAPI(document)
```

O resultado e JSON indentado e deterministico para o mesmo documento.

### Gerar arquivo .http para IntelliJ

```go
payload, err := appRouter.BuildHTTPClient(config, routekit.HTTPClientConfig{
	BaseURL: "https://api.example.com",
})
if err != nil {
	return err
}
if err := os.WriteFile("api.http", payload, 0644); err != nil {
	return err
}
```

Tambem e possivel usar o documento ja construido:

```go
document, err := routekit.BuildOpenAPI(routes, config)
if err != nil {
	return err
}
payload, err := routekit.MarshalHTTPClient(document, routekit.HTTPClientConfig{
	BaseURLVariable: "baseUrl",
	BaseURL:         "https://api.example.com",
})
```

`HTTPClientConfig.BaseURL` tem precedencia sobre `Servers`. Quando `BaseURL` fica vazio, o gerador usa o primeiro server absoluto `http://` ou `https://` do documento. Se nao houver URL absoluta, a geracao retorna erro para evitar requests relativas invalidas no HTTP Client.

`BaseURLVariable` usa `baseUrl` por default e aparece nas requests como `{{baseUrl}}`. Parametros sem exemplo viram placeholders derivados do local e do nome, como `{{pathId}}`, `{{queryPage}}` e `{{headerXRequestID}}`. Parametros e bodies com exemplo usam os valores do OpenAPI; bodies JSON sem exemplo recebem um payload estavel gerado a partir do schema e de `components.schemas`.

O suporte inicial de autenticacao gera headers Bearer e API key em header ou query. OAuth2, OpenID Connect e auth HTTP nao-bearer sao ignorados nesta versao e aparecem como comentario no bloco gerado.

### Registrar o endpoint JSON

```go
err := appRouter.RegisterOpenAPI(engine, config)
```

Path default:

```text
/openapi.json
```

Para alterar:

```go
config.JSONPath = "/spec/openapi.json"
```

O payload e calculado no registro e servido de cache com `Cache-Control: no-store`. Alterar config ou rotas depois nao atualiza automaticamente o endpoint.

`RegisterOpenAPI` nao valida `JSONPath` nem um engine nulo antes de chamar o Gin. Use um path Gin estatico valido, um engine nao nulo e registre cada path apenas uma vez; configuracao invalida ou rota duplicada pode causar panic do Gin.

### Responses obrigatorias

Toda operacao documentada precisa declarar pelo menos uma response. Nao existe `200 OK` implicito.

```go
route.Response(200, "OK", nil)
```

Uma operacao sem response gera erro. Uma operacao com responses, mas sem 2xx, gera warning.

## Schemas, registrations e providers

### SchemaOf

```go
routekit.SchemaOf[UserResponse]()
routekit.SchemaOf[[]UserResponse]()
routekit.SchemaOf[Page[UserResponse]]()
routekit.SchemaOf[*UserResponse]()
```

`SchemaOf` descreve o tipo sem criar um valor.

### SchemaWithExample

```go
routekit.SchemaWithExample(UserResponse{
	ID:   "usr_123",
	Name: "Ada",
})
```

Um `Example` definido diretamente em `DocBody` ou `DocResponse` prevalece sobre o exemplo do descriptor.

### Mapeamentos principais

| Go | OpenAPI |
| --- | --- |
| `string` | `string` |
| `bool` | `boolean` |
| inteiros | `integer` |
| `float32` | `number/float` |
| `float64` | `number/double` |
| `time.Time` | `string/date-time` |
| `json.Number` | `number` |
| `json.RawMessage` | schema livre |
| `[]byte` | `string/byte` |
| slice e array | `array` |
| map | `object` com `additionalProperties` |
| struct | component `object` |
| interface | schema livre |

O reflector tambem considera fields embedded, `json:"-"`, `omitempty`, `omitzero`, `json:",string"`, aliases, generics, recursao e codecs de texto.

### Schemas direcionais

Request e response sao refletidos separadamente.

Request considera:

```text
binding:"required"
UnmarshalText
UnmarshalJSON
null aceito pelo decoder
map keys aceitas no decode
```

Response considera:

```text
omitempty e omitzero
MarshalText
MarshalJSON
valores nil emitidos como null
map keys aceitas no encode
```

Schemas equivalentes podem compartilhar component. Schemas diferentes recebem refs separadas.

### Schema explicito

```go
schema := routekit.OpenAPISchema{
	Type: "object",
	Properties: map[string]routekit.OpenAPISchema{
		"value": {Type: "string"},
	},
	Required: []string{"value"},
}

route.Body(schema)
```

O modelo publico suporta `Type`, `Format`, `Description`, `AnyOf`, `Items`, `Properties`, `Required`, `Ref` e `AdditionalProperties`.

### Components manuais

```go
config.Components.Schemas = map[string]*routekit.OpenAPISchema{
	"Error": {
		Type: "object",
		Properties: map[string]routekit.OpenAPISchema{
			"error": {Type: "string"},
		},
		Required: []string{"error"},
	},
}
```

Nomes de components devem corresponder a `^[A-Za-z0-9._-]+$`.

### Nomear um tipo refletido

```go
config.SchemaRegistrations = []routekit.SchemaRegistration{
	routekit.RegisterSchemaAs[CreateUserRequest](
		"CreateUserInput",
		routekit.ForSchemaRequest(),
	),
}
```

### Override por tipo

```go
config.SchemaRegistrations = []routekit.SchemaRegistration{
	routekit.OverrideSchemaOf[ExternalDate](
		routekit.OpenAPISchema{Type: "string", Format: "date"},
		routekit.ForSchemaDirections(
			routekit.SchemaRequest,
			routekit.SchemaResponse,
		),
	),
}
```

Opcoes:

```go
routekit.ForSchemaRequest()
routekit.ForSchemaResponse()
routekit.ForSchemaDirections(routekit.SchemaRequest, routekit.SchemaResponse)
```

Sem opcao, o registro e fallback para ambas as direcoes.

### Provider comum

```go
func (ExternalDate) OpenAPISchema() routekit.OpenAPISchema {
	return routekit.OpenAPISchema{Type: "string", Format: "date"}
}
```

### Provider direcional

```go
func (EncodedValue) OpenAPISchemaFor(
	direction routekit.SchemaDirection,
) routekit.OpenAPISchema {
	if direction == routekit.SchemaRequest {
		return routekit.OpenAPISchema{Type: "string"}
	}
	return routekit.OpenAPISchema{Type: "object"}
}
```

Providers com pointer receiver sao chamados sobre uma nova instancia zero nao nula. Panic vira diagnostico.

Tipos com `MarshalJSON` ou `UnmarshalJSON` arbitrario precisam de schema explicito, override ou provider. O reflector nao executa codecs para tentar adivinhar o formato.

## Security schemes e regras

### Bearer token

```go
config.Components.SecuritySchemes = map[string]*routekit.OpenAPISecurityScheme{
	"BearerAuth": routekit.BearerSecurityScheme("JWT access token"),
}
```

### API key em header

```go
config.Components.SecuritySchemes = map[string]*routekit.OpenAPISecurityScheme{
	"APIKey": routekit.APIKeyHeaderSecurityScheme("X-API-Key", "API key"),
}
```

### API key em query

```go
config.Components.SecuritySchemes = map[string]*routekit.OpenAPISecurityScheme{
	"APIKey": routekit.APIKeyQuerySecurityScheme("api_key", "API key"),
}
```

### OAuth2

```go
config.Components.SecuritySchemes = map[string]*routekit.OpenAPISecurityScheme{
	"OAuth": {
		Type: "oauth2",
		Flows: &routekit.OpenAPIOAuthFlows{
			AuthorizationCode: &routekit.OpenAPIOAuthFlow{
				AuthorizationURL: "https://auth.example.com/authorize",
				TokenURL:         "https://auth.example.com/token",
				Scopes: map[string]string{
					"users:read":  "Read users",
					"users:write": "Write users",
				},
			},
		},
	},
}
```

### Aplicar security requirement

Por middleware documentado:

```go
routekit.RequireSecurityScheme("BearerAuth")
```

Por profile:

```go
DocProfile{Security: []string{"BearerAuth"}}
```

Por decorator:

```go
ctx.Operation.AddSecurity("BearerAuth")
```

### AND e OR

```go
ctx.Operation.AddSecurityRequirement("ClientID", "ClientSecret")
ctx.Operation.AddSecurity("BearerAuth")
```

Semantica:

```text
(ClientID AND ClientSecret) OR BearerAuth
```

Cada item do slice `Security` e uma alternativa OR. Varios schemes no mesmo map sao AND.

### Regras baseadas nas flags

```go
config.SecurityRules = []routekit.RouteSecurityRule{
	routekit.WhenAuthenticated(
		routekit.RequireSecurityScheme("BearerAuth"),
	),
	routekit.WhenM2M(
		routekit.RequireSecurityScheme("ClientCredentials"),
	),
	routekit.WhenIntegration(
		routekit.RequiresHeader("X-Integration-ID", "Integration identifier"),
	),
}
```

Predicado customizado:

```go
rule := routekit.NewRouteSecurityRule(
	func(route routekit.Route, handler routekit.Handler) bool {
		return len(handler.Scopes) > 0
	},
	routekit.RequireSecurityScheme("OAuth", "users:read"),
)
```

Security scheme e operation header sao conceitos diferentes. Um API key em header nao cria automaticamente um `OpenAPIParameter`. Declarar o mesmo header nas duas formas para a mesma operacao e erro.

## Decorators

Decorators alteram a operacao OpenAPI depois de defaults, profiles, middleware, security rules, contract e fluents.

```go
config.RouteDecorators = []routekit.RouteDocDecorator{
	func(ctx *routekit.RouteDocContext) error {
		ctx.Operation.AddHeader(
			"X-Request-ID",
			"string",
			true,
			"Request identifier",
		)
		ctx.Operation.AddResponse(
			429,
			"Too Many Requests",
			routekit.SchemaOf[ErrorResponse](),
		)
		return nil
	},
}
```

Helpers da operacao:

```go
operation.AddHeader(name, typ, required, description)
operation.AddQuery(name, typ, required, description)
operation.AddPathParam(name, typ, description)
operation.AddParameter(parameter)
operation.AddResponse(status, description, schema)
operation.AddSecurity(name)
operation.AddSecurityRequirement(names...)
operation.AddSecurityRequirementFromMap(requirement)
```

O contexto contem:

```go
type RouteDocContext struct {
	Route     routekit.Route
	Handler   routekit.Handler
	Operation *routekit.OpenAPIOperation
	Config    routekit.OpenAPIConfig
}
```

Erro retornado ou panic de decorator vira diagnostico e bloqueia o build. Decorators seguintes daquela rota nao executam depois do primeiro erro.

## Swagger UI

Registrar com defaults:

```go
err := appRouter.RegisterSwaggerUI(engine, routekit.SwaggerUIConfig{})
```

Defaults:

```text
Path:       /docs
OpenAPIURL: /openapi.json
Title:      API Docs
CDNBaseURL: https://unpkg.com/swagger-ui-dist
```

Configuracao para spec unica:

```go
err := appRouter.RegisterSwaggerUI(engine, routekit.SwaggerUIConfig{
	Path:       "/documentation",
	OpenAPIURL: "/spec/openapi.json",
	Title:      "Example API",
	CDNBaseURL: "https://unpkg.com/swagger-ui-dist",
})
```

Configuracao para multiplas specs:

```go
err := appRouter.RegisterSwaggerUI(engine, routekit.SwaggerUIConfig{
	Path:  "/docs",
	Title: "Core API Documentation",
	OpenAPIURLs: []routekit.SwaggerUISpec{
		{Name: "Core API", URL: "/openapi.json"},
		{Name: "Attendance System", URL: "/docs/spec/attendance-system"},
		{Name: "EvoBridge", URL: "/docs/spec/evobridge-middleware"},
	},
	PrimaryOpenAPIName: "Core API",
})
```

`OpenAPIURLs` ativa o seletor nativo de APIs da Swagger UI via opcao `urls`. Se `PrimaryOpenAPIName` ficar vazio, a primeira spec vira a primaria. Quando `OpenAPIURLs` estiver preenchido, `OpenAPIURL` e ignorado.

URLs externas precisam de CORS habilitado no servidor da especificacao porque a Swagger UI roda no navegador. A biblioteca nao busca nem faz proxy server-side das especificacoes.

Regras:

- `Path` precisa comecar com `/`.
- `Path` nao pode conter parametros `:id` nem wildcards `*path`.
- Em modo de spec unica, `OpenAPIURL` precisa comecar com `/`, `http://` ou `https://`. O helper nao faz parsing completo da URL.
- Em modo de multiplas specs, cada item de `OpenAPIURLs` precisa ter `Name` obrigatorio e unico.
- Em modo de multiplas specs, cada `URL` precisa comecar com `/`, `http://` ou `https://`.
- Em modo de multiplas specs, `PrimaryOpenAPIName`, quando informado, precisa corresponder a um `Name` existente.
- `CDNBaseURL` precisa comecar com `http://` ou `https://`.
- O helper usa arquivos do CDN no navegador; ambientes sem acesso externo precisam configurar um CDN acessivel.
- Registrar Swagger UI nao registra o JSON OpenAPI. Use tambem `RegisterOpenAPI` ou informe outra `OpenAPIURL`.

## Stoplight Elements

Alternativa a Swagger UI com layout de tres colunas. Registra uma pagina HTML que carrega o Stoplight Elements via CDN e aponta para o JSON OpenAPI.

Registrar com defaults:

```go
err := appRouter.RegisterStoplightUI(engine, routekit.StoplightUIConfig{})
```

Defaults:

```text
Path:       /docs
OpenAPIURL: /openapi.json
Title:      API Docs
Layout:     sidebar
Router:     hash
CDNBaseURL: https://cdn.jsdelivr.net/npm/@stoplight/elements@9.0.24
```

Configuracao customizada:

```go
err := appRouter.RegisterStoplightUI(engine, routekit.StoplightUIConfig{
	Path:       "/documentation",
	OpenAPIURL: "/spec/openapi.json",
	Title:      "Example API",
	Layout:     routekit.StoplightLayoutResponsive,
	Logo:       "/assets/logo.png",
	HideTryIt:  true,
})
```

Layouts disponiveis:

- `routekit.StoplightLayoutSidebar` (default): navegacao lateral.
- `routekit.StoplightLayoutResponsive`: layout responsivo.
- `routekit.StoplightLayoutStacked`: conteudo empilhado.

Routers disponiveis:

- `routekit.StoplightRouterHash` (default): deep linking via fragmento `#`.
- `routekit.StoplightRouterHistory`: deep linking via History API. Injeta automaticamente `basePath` igual a `Path`.
- `routekit.StoplightRouterMemory`: roteamento em memoria.
- `routekit.StoplightRouterStatic`: pagina estatica.

As opcoes `HideExport`, `HideSchemas`, `HideTryIt` e `HideTryItPanel` sao propriedades do web component aplicadas via JavaScript apos o carregamento.

Limitacoes em relacao a Swagger UI:

- Spec unica apenas: Stoplight Elements nao tem seletor nativo de multiplas especificacoes. Para multiplas specs use `RegisterSwaggerUI` com `OpenAPIURLs`.
- Path default igual ao do Swagger UI (`/docs`): registrar ambos exige customizar o path de um deles, senao o Gin entra em panic de rota duplicada.

Regras:

- `Path` precisa comecar com `/`.
- `Path` nao pode conter parametros `:id` nem wildcards `*path`.
- `OpenAPIURL` precisa comecar com `/`, `http://` ou `https://`.
- `Layout` precisa ser `sidebar`, `responsive` ou `stacked`.
- `Router` precisa ser `hash`, `history`, `memory` ou `static`.
- `CDNBaseURL` precisa comecar com `http://` ou `https://`.
- O helper usa arquivos do CDN no navegador; ambientes sem acesso externo precisam configurar um CDN acessivel. A versao default e fixa (`@9.0.24`); atualizar a versao exige mudar `CDNBaseURL`.
- URLs externas de especificacao precisam de CORS habilitado porque o Stoplight Elements roda no navegador.
- Registrar Stoplight UI nao registra o JSON OpenAPI. Use tambem `RegisterOpenAPI` ou informe outra `OpenAPIURL`.

## jsonendpoint

O subpacote `jsonendpoint` cria handlers JSON tipados e deriva o `Contract` da mesma configuracao runtime.

### Endpoint basico

```go
route := jsonendpoint.Handle(
	group,
	http.MethodPost,
	"/users",
	func(c *gin.Context, request CreateUserRequest) (UserResponse, error) {
		return service.CreateUser(c.Request.Context(), request)
	},
	"Create user",
	700,
	jsonendpoint.WithSuccess(http.StatusCreated, "Created"),
)

route.Public().Tags("users")
```

O retorno e o mesmo `*routekit.RouteConfig`, portanto todos os fluents continuam disponiveis.

### Defaults runtime

```text
body obrigatorio
Content-Type application/json ou +json
limite de 1 MiB
status 200
erro de binding/validacao: 400 {"error":"invalid request"}
erro do handler/marshal:     500 {"error":"internal server error"}
```

### Opcoes

```go
jsonendpoint.WithSuccess(status, description)
jsonendpoint.WithOptionalBody()
jsonendpoint.WithValidator(validator)
jsonendpoint.WithBodyLimit(bytes)
jsonendpoint.WithStrictWriter()
```

### Validator de aplicacao

```go
func validateCreateUser(c *gin.Context, request CreateUserRequest) error {
	if request.Email == "blocked@example.com" {
		return errors.New("blocked email")
	}
	return nil
}

jsonendpoint.Handle(
	group,
	http.MethodPost,
	"/users",
	handler,
	"Create user",
	701,
	jsonendpoint.WithValidator(validateCreateUser),
)
```

Tags `binding` do Gin executam antes dos validators de aplicacao.

### Body opcional

```go
jsonendpoint.WithOptionalBody()
```

Ausencia de body entrega o zero value do request. Para ponteiro, slice e map, o zero value e `nil`.

JSON `null` nao e ausencia. Ele e um valor presente e segue `encoding/json`:

```text
*Request -> nil
slice     -> nil
map       -> nil
struct    -> zero value
int       -> mantem zero value
```

Tags `binding:"required"` ou validators podem rejeitar o valor resultante.

### Responses nil

Ponteiros, slices e maps nil sao serializados como JSON `null`.

```go
func list(c *gin.Context, request ListRequest) ([]Item, error) {
	return nil, nil // HTTP 200 com body null
}
```

Para retornar `[]`, inicialize `[]Item{}`. Para retornar `{}`, inicialize o map.

Status 204 e 205 nunca escrevem body.

### Handler escrevendo diretamente

O handler tipado deve retornar um valor em vez de chamar `c.JSON` ou `c.Data`.

Se escrever diretamente, o adapter nao escreve uma segunda response. Com `WithStrictWriter`, registra `jsonendpoint.ErrResponseAlreadyWritten` em `gin.Context.Errors`.

```go
func errorObserver(c *gin.Context) {
	c.Next()
	for _, contextError := range c.Errors {
		if errors.Is(contextError.Err, jsonendpoint.ErrResponseAlreadyWritten) {
			logger.Error("typed handler wrote directly")
		}
	}
}
```

### Tipos suportados

O runtime segue `encoding/json`: structs, ponteiros, slices, maps, aliases, generics, fields embedded, bytes, `json.RawMessage`, `json.Number`, containers recursivos e `json:",string"`.

Codecs JSON arbitrarios podem funcionar no runtime, mas precisam de schema explicito, override ou provider para o OpenAPI.

### Metodos e status

`jsonendpoint` rejeita `HEAD`, `TRACE`, `CONNECT` e qualquer metodo ausente de `routekit.ValidMethods`. O status de sucesso precisa estar entre 200 e 299. O body limit precisa estar entre 1 e `math.MaxInt64-1`.

Configuracao estatica invalida causa panic durante `jsonendpoint.Handle`, pois essa funcao nao possui retorno de erro. Isso inclui grupo, handler, option ou validator nulo, metodo/status/limite invalido e validator cujo tipo nao corresponde ao request.

### Quando nao usar jsonendpoint

Use handlers Gin tradicionais para:

- streaming;
- Server-Sent Events;
- WebSocket;
- proxy/pass-through;
- downloads;
- multipart/upload;
- handlers que escrevem varias respostas;
- fluxos que precisam controlar o writer diretamente.

O guia especializado esta em [jsonendpoint.md](docs/jsonendpoint.md).

## Validacao, testes e CI

### Validar sem servidor

As funcoes de pacote aceitam qualquer snapshot de rotas:

```go
report, err := routekit.ValidateOpenAPI(routes, config)
document, err := routekit.BuildOpenAPI(routes, config)
payload, err := routekit.MarshalOpenAPI(document)
```

Isso permite testes e snapshots sem registrar `/openapi.json`.

### Testar um registrar

```go
func TestUserRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()

	route := (UserRoutes{}).Register(engine)
	if len(route.Handlers) != 3 {
		t.Fatalf("handlers = %d", len(route.Handlers))
	}

	document, err := routekit.BuildOpenAPI(
		[]routekit.Route{route},
		routekit.OpenAPIConfig{
			Title:             "Test",
			Version:           "1",
			BasePath:          "/api",
			PathMode:          routekit.PathsRelativeToBase,
			DocumentationMode: routekit.DocumentAll,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if document.Paths["/users"] == (routekit.OpenAPIPathItem{}) {
		t.Fatal("missing /users")
	}
}
```

### CI recomendado

```bash
gofmt -w $(git ls-files '*.go')
go test ./...
go test -race ./...
go vet ./...
```

Para falhar a pipeline em qualquer erro OpenAPI:

```go
report, err := routekit.ValidateOpenAPI(routes, config)
if err != nil {
	return fmt.Errorf("invalid OpenAPI (%d diagnostics): %w", len(report.Diagnostics), err)
}
```

## Erros comuns

### Build reclama de BasePath ou PathMode

Os dois campos sao obrigatorios:

```go
BasePath: "/api",
PathMode: routekit.PathsRelativeToBase,
```

### Operacao sem response

Nao existe response implicita:

```go
route.Response(200, "OK", nil)
```

Ou use um contract.

### Placeholder sem parametro

Para `/users/:id`:

```go
route.PathParam("id", "string", true, "User ID")
```

### Security scheme ausente

Todo requirement precisa apontar para `Components.SecuritySchemes`:

```go
config.Components.SecuritySchemes = map[string]*routekit.OpenAPISecurityScheme{
	"BearerAuth": routekit.BearerSecurityScheme("JWT"),
}
```

### API key duplicada como header

Nao declare o mesmo header simultaneamente como security scheme e operation parameter. O scheme ja informa ao Swagger como enviar a chave.

### Codec JSON exige provider

Um tipo com `MarshalJSON` ou `UnmarshalJSON` arbitrario nao pode ser inferido com seguranca. Use `OverrideSchemaOf`, provider ou schema explicito.

### RegisterRoutes precisa ser chamado

Estes metodos exigem snapshot registrado:

```go
appRouter.ValidateOpenAPI(config)
appRouter.BuildOpenAPI(config)
appRouter.RegisterOpenAPI(engine, config)
```

Execute primeiro:

```go
appRouter.RegisterRoutes(engine)
```

### OpenAPI URL e JSONPath

`JSONPath` nao e combinado automaticamente com `BasePath`. Se o JSON esta em `/spec/openapi.json`, configure o Swagger UI com a mesma URL.

### Warnings nao bloqueiam BuildOpenAPI

Use `ValidateOpenAPI` para observar warnings. `BuildOpenAPI` bloqueia apenas diagnosticos de severidade Error.

## Referencia rapida

### Rotas

```go
routekit.NewRouterGroup
RouterGroup.GET
RouterGroup.HEAD
RouterGroup.POST
RouterGroup.PUT
RouterGroup.PATCH
RouterGroup.DELETE
RouterGroup.OPTIONS
RouterGroup.Handle
RouterGroup.Use
RouterGroup.UseDocumented
RouterGroup.Export
```

### Fluents runtime

```go
RouteConfig.Public
RouteConfig.NoAuthz
RouteConfig.RequireClientContext
RouteConfig.NoClientContext
RouteConfig.BasicRoute
RouteConfig.M2MRoute
RouteConfig.AllowAnySessionApp
RouteConfig.IntegrationRoute
RouteConfig.Scopes
RouteConfig.Use
RouteConfig.UseDocumented
```

### Fluents documentais

```go
RouteConfig.Document
RouteConfig.HideFromDocs
RouteConfig.Summary
RouteConfig.Description
RouteConfig.OperationID
RouteConfig.Tags
RouteConfig.DocProfile
RouteConfig.Header
RouteConfig.Query
RouteConfig.PathParam
RouteConfig.Body
RouteConfig.BodyWith
RouteConfig.Response
RouteConfig.ResponseWith
RouteConfig.Contract
RouteConfig.WithoutDocProfile
RouteConfig.WithoutDefaultResponse
RouteConfig.WithoutDefaultParameter
```

### Opcoes do grupo

```go
routekit.WithAuthMiddlewareFactory
routekit.WithAuthorizationMiddleware
routekit.WithSameApplicationMiddleware
routekit.WithRouteContextKeys
routekit.WithDocumentation
routekit.WithDocProfiles
routekit.WithDefaultHeader
routekit.WithDefaultQuery
routekit.WithDefaultPathParam
routekit.WithDefaultResponse
routekit.WithDefaultResponseWith
routekit.WithDefaultContentTypes
```

### AppRouter

```go
routekit.NewAppRouter
routekit.NewAppRouterFromRegistrars
AppRouter.RegisterRoutes
AppRouter.Routes
AppRouter.ValidateOpenAPI
AppRouter.BuildOpenAPI
AppRouter.BuildHTTPClient
AppRouter.RegisterOpenAPI
AppRouter.RegisterSwaggerUI
AppRouter.RegisterStoplightUI
```

### OpenAPI

```go
routekit.ValidateOpenAPI
routekit.BuildOpenAPI
routekit.MarshalOpenAPI
routekit.MarshalHTTPClient
routekit.HTTPClientConfig
routekit.DiagnosticReport.HasErrors
routekit.SchemaOf
routekit.SchemaWithExample
routekit.RegisterSchemaAs
routekit.OverrideSchemaOf
routekit.BearerSecurityScheme
routekit.APIKeyHeaderSecurityScheme
routekit.APIKeyQuerySecurityScheme
```

### Contracts

```go
routekit.JSONRequestContractOf
routekit.JSONResponseContractOf
routekit.EmptyJSONResponseContract
routekit.JSONContractOf
routekit.DescribeJSON
routekit.ResponseOf
routekit.DefaultResponseOf
routekit.WithJSONDefaults
```

### Contribuicoes e security rules

```go
routekit.RequireSecurityScheme
routekit.RequireOperationHeader
routekit.RequiresHeader
routekit.RequiresSecurity
routekit.RespondsWith
routekit.UsesProfiles
routekit.NewRouteSecurityRule
routekit.WhenAuthenticated
routekit.WhenM2M
routekit.WhenIntegration
```

### jsonendpoint

```go
jsonendpoint.Handle
jsonendpoint.WithSuccess
jsonendpoint.WithOptionalBody
jsonendpoint.WithValidator
jsonendpoint.WithBodyLimit
jsonendpoint.WithStrictWriter
```

## Leitura adicional

- [README principal](README.md)
- [Guia especializado do jsonendpoint](docs/jsonendpoint.md)
- [Migracao para OpenAPI 3.1](migration-openapi-3.1.md)
- [ADR do endpoint JSON tipado](docs/decisions/0001-typed-json-endpoint.md)
