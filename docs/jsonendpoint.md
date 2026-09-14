# Guia de uso do jsonendpoint

O pacote `jsonendpoint` oferece handlers JSON tipados para Gin. Ele usa os mesmos tipos e opcoes para executar o endpoint e gerar seu `routekit.Contract`, reduzindo divergencias entre o comportamento runtime e a documentacao OpenAPI.

Esta API e opcional. Handlers Gin tradicionais continuam suportados e devem ser usados para streaming, SSE, WebSocket, proxy, downloads, multipart e outros fluxos que controlam a resposta manualmente.

## Importacao

```go
import (
	"net/http"

	"github.com/gin-gonic/gin"
	routekit "github.com/spirandev/gin-routekit"
	"github.com/spirandev/gin-routekit/jsonendpoint"
)
```

## Primeiro endpoint

Defina os DTOs de request e response:

```go
type CreateUserRequest struct {
	Name  string `json:"name" binding:"required"`
	Email string `json:"email" binding:"required,email"`
}

type CreateUserResponse struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}
```

Registre o endpoint:

```go
route := jsonendpoint.Handle(
	group,
	http.MethodPost,
	"/users",
	func(c *gin.Context, request CreateUserRequest) (CreateUserResponse, error) {
		user, err := service.CreateUser(c.Request.Context(), request.Name, request.Email)
		if err != nil {
			return CreateUserResponse{}, err
		}

		return CreateUserResponse{
			ID:    user.ID,
			Name:  user.Name,
			Email: user.Email,
		}, nil
	},
	"Create user",
	1001,
	jsonendpoint.WithSuccess(http.StatusCreated, "Created"),
)

route.Public().Tags("users")
```

`Handle` retorna o mesmo `*routekit.RouteConfig` usado pela API tradicional. Todos os fluents existentes continuam disponiveis, incluindo:

```go
route.Public()
route.NoAuthz()
route.RequireClientContext()
route.Scopes("users:write")
route.Use(middleware)
route.UseDocumented(middleware, metadata)
route.Document()
route.Tags("users")
```

## Assinatura do handler

O handler tipado segue esta assinatura:

```go
type Handler[Request, Response any] func(
	*gin.Context,
	Request,
) (Response, error)
```

Os tipos genericos normalmente sao inferidos automaticamente, inclusive para method values:

```go
type UserHandler struct {
	service UserService
}

func (h UserHandler) Create(
	c *gin.Context,
	request CreateUserRequest,
) (CreateUserResponse, error) {
	// ...
}

jsonendpoint.Handle(
	group,
	http.MethodPost,
	"/users",
	userHandler.Create,
	"Create user",
	1001,
)
```

## Comportamento padrao

Sem opcoes adicionais, o endpoint possui o seguinte comportamento:

| Item | Padrao |
| --- | --- |
| Request body | obrigatorio |
| Content-Type aceito | `application/json` ou tipo com sufixo `+json` |
| Limite do body | 1 MiB |
| Status de sucesso | 200 |
| Response de sucesso | JSON direto, sem envelope |
| Erro de binding/validacao | 400 com `{"error":"invalid request"}` |
| Erro retornado pelo handler | 500 com `{"error":"internal server error"}` |
| Documentacao automatica | respeita o modo documental configurado |

Um body vazio e rejeitado quando obrigatorio. JSON `null` e diferente de body ausente: ele conta como um valor presente e segue a semantica de `encoding/json`. Por exemplo, `null` produz `nil` em requests ponteiro, slice ou map e mantem o zero value de valores nao anulaveis. Tags `binding` e validators ainda podem rejeitar o valor Go resultante.

## Status de sucesso

Use `WithSuccess` para definir o status e a descricao documentada:

```go
jsonendpoint.Handle(
	group,
	http.MethodPost,
	"/users",
	handler,
	"Create user",
	1001,
	jsonendpoint.WithSuccess(http.StatusCreated, "Created"),
)
```

Somente status entre 200 e 299 sao aceitos. Status 204 e 205 nunca serializam o valor retornado pelo handler e sao documentados sem response body:

```go
route := jsonendpoint.Handle(
	group,
	http.MethodDelete,
	"/users/:id",
	handler,
	"Delete user",
	1002,
	jsonendpoint.WithSuccess(http.StatusNoContent, "No Content"),
)
route.PathParam("id", "string", true, "User ID")
```

## Body opcional

Use `WithOptionalBody` quando a ausencia completa do body for valida:

```go
jsonendpoint.Handle(
	group,
	http.MethodPost,
	"/jobs/run",
	handler,
	"Run job",
	2001,
	jsonendpoint.WithOptionalBody(),
)
```

Quando o body esta ausente, o handler recebe o zero value do tipo de request. Isso inclui `nil` para ponteiros, slices e maps:

```go
func handler(
	c *gin.Context,
	request *RunJobRequest,
) (RunJobResponse, error) {
	// request e nil quando o body opcional esta ausente.
}
```

`WithOptionalBody` controla somente a ausencia do body. Tanto no modo obrigatorio quanto no opcional, um body presente contendo `null` e decodificado normalmente por `encoding/json`.

## Validacao adicional

Tags `binding` sao processadas pelo binder JSON do Gin. Para regras de aplicacao, use `WithValidator`:

```go
func validateCreateUser(
	c *gin.Context,
	request CreateUserRequest,
) error {
	if strings.HasSuffix(request.Email, "@blocked.example") {
		return errors.New("email domain is blocked")
	}
	return nil
}

jsonendpoint.Handle(
	group,
	http.MethodPost,
	"/users",
	handler,
	"Create user",
	1001,
	jsonendpoint.WithValidator(validateCreateUser),
)
```

Erros do validator usam a mesma policy de erros de binding e produzem status 400. O validator e executado depois do binding do Gin e nao executa novamente as validacoes declaradas em `binding`.

Mais de um validator pode ser registrado. Eles sao executados na ordem das opcoes e param no primeiro erro:

```go
jsonendpoint.Handle(
	group,
	http.MethodPost,
	"/users",
	handler,
	"Create user",
	1001,
	jsonendpoint.WithValidator(validateDomain),
	jsonendpoint.WithValidator(validatePlan),
)
```

## Limite do request body

O limite padrao e 1 MiB. Use `WithBodyLimit` para altera-lo:

```go
jsonendpoint.Handle(
	group,
	http.MethodPost,
	"/events",
	handler,
	"Create event",
	3001,
	jsonendpoint.WithBodyLimit(256*1024), // 256 KiB
)
```

O limite deve estar entre 1 e `math.MaxInt64-1`. Um body maior que o limite produz a resposta de binding 400 e o handler nao e executado.

## Handler que escreve diretamente

O handler tipado deve retornar um valor em vez de escrever a response diretamente. Se ele escrever usando `c.JSON`, `c.Data` ou o writer do Gin, o adapter detecta que a response ja foi iniciada e nao escreve uma segunda response.

Durante migracoes, `WithStrictWriter` registra essa violacao em `gin.Context.Errors`:

```go
jsonendpoint.Handle(
	group,
	http.MethodPost,
	"/users",
	handler,
	"Create user",
	1001,
	jsonendpoint.WithStrictWriter(),
)
```

Middlewares de observabilidade podem detectar `jsonendpoint.ErrResponseAlreadyWritten` depois de `c.Next()`:

```go
func contractViolationLogger(c *gin.Context) {
	c.Next()

	for _, contextError := range c.Errors {
		if errors.Is(contextError.Err, jsonendpoint.ErrResponseAlreadyWritten) {
			logger.Error("typed endpoint wrote directly to Gin writer")
		}
	}
}
```

Mesmo sem modo estrito, o adapter nunca escreve uma segunda response.

## Erros e observabilidade

Erros de binding, validators, handler e serializacao sao adicionados a `gin.Context.Errors`. Um middleware pode registrar a causa real sem expo-la ao cliente:

```go
func errorLogger(c *gin.Context) {
	c.Next()

	for _, contextError := range c.Errors {
		logger.Error("request failed", "error", contextError.Err)
	}
}
```

As respostas default nao expoem detalhes internos:

```json
{"error":"invalid request"}
```

```json
{"error":"internal server error"}
```

Policies de erro customizadas ainda nao fazem parte da fase minima da API.

## Tipos de request e semantica JSON

O tipo de request pode ser qualquer tipo que `encoding/json` consiga decodificar e cujo schema possa ser representado ou fornecido ao gerador OpenAPI. Isso inclui structs, ponteiros, slices, maps, aliases, tipos genericos, containers recursivos, campos embedded promovidos, `json.RawMessage`, `json.Number`, sequencias de bytes e campos com `json:",string"`.

Exemplos:

```go
func structHandler(c *gin.Context, request CreateUserRequest) (Response, error)
func pointerHandler(c *gin.Context, request *CreateUserRequest) (Response, error)
func sliceHandler(c *gin.Context, request []CreateUserRequest) (Response, error)
func mapHandler(c *gin.Context, request map[string]int) (Response, error)
```

Maps seguem as chaves aceitas por `encoding/json`: strings, inteiros e tipos que implementam o codec de texto apropriado. Interfaces sao representadas por schema JSON livre; use schema explicito quando o contrato real for mais restrito.

Campos aceitos no request sao analisados separadamente dos campos emitidos na response. No request, `binding:"required"` define `required`; `null` ainda segue o comportamento do decoder e da validacao Gin.

## Tipos de response e valores nil

A response pode ser qualquer tipo serializavel por `encoding/json`, observadas as necessidades documentais descritas abaixo. O adapter chama `json.Marshal` diretamente.

Ponteiros, slices e maps nil sao valores JSON validos e serializam como `null`, tanto no nivel superior quanto em campos aninhados. Se o contrato exige array ou objeto vazio em vez de `null`, o handler deve inicializar o valor:

```go
return ListUsersResponse{
	Users: []UserResponse{},
}, nil
```

Campos com `omitempty` podem continuar omitindo containers vazios ou nil.

Status 204 e 205 sao a excecao: o adapter ignora o valor retornado e nao escreve body. Falhas reais de `json.Marshal`, como ciclos ou valores nao suportados, produzem a policy 500 e sao registradas em `gin.Context.Errors`.

## Fidelidade do schema

O contrato derivado usa `SchemaOf[Request]` na direcao de request e `SchemaOf[Response]` na direcao de response. O gerador OpenAPI considera promocao e conflitos de campos embedded, tags `json`, `omitempty`, `omitzero`, `json:",string"`, nulabilidade, bytes em base64, `json.RawMessage`, `json.Number`, aliases, generics, recursao e codecs de texto.

Metodos `MarshalJSON` e `UnmarshalJSON` arbitrarios podem produzir qualquer shape e nao sao executados em zero values para adivinhar o contrato. Esses tipos precisam de uma destas declaracoes:

- schema explicito em um escape hatch documental;
- `routekit.OverrideSchemaOf[T]` em `OpenAPIConfig.SchemaRegistrations`;
- implementacao de `routekit.OpenAPISchemaProvider`;
- implementacao de `routekit.DirectionalOpenAPISchemaProvider` quando request e response diferem.

Exemplo de override apenas para response:

```go
config.SchemaRegistrations = []routekit.SchemaRegistration{
	routekit.OverrideSchemaOf[EncodedID](
		routekit.OpenAPISchema{Type: "string", Format: "uuid"},
		routekit.ForSchemaResponse(),
	),
}
```

Um shape nao representavel, codec JSON sem declaracao, provider invalido ou kind sem representacao gera diagnostico em `ValidateOpenAPI`/`BuildOpenAPI`, quando a configuracao OpenAPI esta disponivel. `jsonendpoint.Handle` continua causando panic com prefixo `jsonendpoint:` apenas para configuracao estatica invalida do adapter, como metodo/status/limite invalido, option nil ou validator com tipo incorreto.

## OpenAPI

O `Contract` anexado automaticamente contem:

- request body com o tipo generico de request;
- content type `application/json`;
- status e schema de sucesso;
- erro de binding 400;
- erro de handler 500.

O endpoint tipado respeita os modos documentais existentes. Por exemplo, para documentar todas as rotas:

```go
document, err := routekit.BuildOpenAPI(routes, routekit.OpenAPIConfig{
	Title:             "Users API",
	Version:           "1.0.0",
	BasePath:          "/api",
	PathMode:          routekit.PathsRelativeToBase,
	DocumentationMode: routekit.DocumentAll,
})
```

`BasePath` e `PathMode` sao obrigatorios. Use `ValidateOpenAPI` antes do build para receber todos os erros e warnings de schemas e operacoes em um unico relatorio:

```go
report, err := routekit.ValidateOpenAPI(routes, config)
if err != nil {
	return fmt.Errorf("OpenAPI invalido (%d diagnosticos): %w", len(report.Diagnostics), err)
}
document, err := routekit.BuildOpenAPI(routes, config)
```

Para opt-in por endpoint:

```go
route := jsonendpoint.Handle(
	group,
	http.MethodPost,
	"/users",
	handler,
	"Create user",
	1001,
)

route.Document()
```

Evite chamar `Contract`, `Body`, `BodyWith`, `Response` ou `ResponseWith` depois de `jsonendpoint.Handle`. Esses fluents sao escape hatches documentais e podem sobrescrever o contrato derivado sem alterar o comportamento runtime.

## Middlewares e autenticacao

Como o retorno e um `*routekit.RouteConfig`, a configuracao continua igual a uma rota tradicional:

```go
jsonendpoint.Handle(
	group,
	http.MethodPost,
	"/reports",
	handler,
	"Create report",
	4001,
).
	Scopes("reports:write").
	RequireClientContext().
	UseDocumented(
		rateLimitMiddleware,
		routekit.RespondsWith(
			routekit.ResponseOf[jsonendpoint.ErrorResponse](
				http.StatusTooManyRequests,
				"Too Many Requests",
			),
		),
	)
```

O adapter nao altera a ordem de middlewares do `RouterGroup`.

## Testando um endpoint

O endpoint so e registrado no Gin quando o grupo e exportado. Um teste runtime deve chamar `Export` antes de `ServeHTTP`:

```go
func TestCreateUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	group := routekit.NewRouterGroup(engine, "/api")

	jsonendpoint.Handle(
		group,
		http.MethodPost,
		"/users",
		func(c *gin.Context, request CreateUserRequest) (CreateUserResponse, error) {
			return CreateUserResponse{ID: "user-1", Name: request.Name}, nil
		},
		"Create user",
		1001,
		jsonendpoint.WithSuccess(http.StatusCreated, "Created"),
	)

	group.Export("users", 1)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/users",
		strings.NewReader(`{"name":"Ada","email":"ada@example.com"}`),
	)
	request.Header.Set("Content-Type", "application/json")

	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusCreated)
	}
}
```

## Quando usar Contract manual

Use `gin.HandlerFunc` com `Contract` manual quando o endpoint:

- usa streaming ou SSE;
- faz upgrade para WebSocket;
- atua como proxy ou pass-through;
- envia arquivos grandes;
- processa multipart;
- pode escrever varias respostas;
- precisa controlar status dinamicos ou headers de response nesta fase.

Exemplo:

```go
routekit.DescribeJSON[LegacyRequest, LegacyResponse](
	group.POST("/legacy", legacyHandler, "Legacy operation", 9001),
	http.StatusOK,
	"OK",
)
```

`Contract` em handlers legados e uma declaracao documental. A garantia estrutural entre runtime e OpenAPI existe apenas no adapter tipado e dentro das restricoes descritas neste guia.
