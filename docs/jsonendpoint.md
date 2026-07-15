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

Um body vazio e rejeitado quando obrigatorio. JSON `null` e diferente de body ausente e tambem e rejeitado porque o schema de request nao e nullable.

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
jsonendpoint.Handle(
	group,
	http.MethodDelete,
	"/users/:id",
	handler,
	"Delete user",
	1002,
	jsonendpoint.WithSuccess(http.StatusNoContent, "No Content"),
)
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

Quando o body esta ausente, structs, slices e maps recebem seu zero value. Requests do tipo ponteiro para struct permanecem alocados:

```go
func handler(
	c *gin.Context,
	request *RunJobRequest,
) (RunJobResponse, error) {
	// request nao e nil, mesmo quando o body opcional esta ausente.
}
```

`WithOptionalBody` nao permite JSON `null`. Ele permite somente a ausencia do body.

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

Erros de binding, validators, handler, validacao da response e serializacao sao adicionados a `gin.Context.Errors`. Um middleware pode registrar a causa real sem expo-la ao cliente:

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

## Tipos de request suportados

O tipo de request pode ser:

- struct;
- ponteiro para struct;
- slice;
- map com chave string.

Exemplos:

```go
func structHandler(c *gin.Context, request CreateUserRequest) (Response, error)
func pointerHandler(c *gin.Context, request *CreateUserRequest) (Response, error)
func sliceHandler(c *gin.Context, request []CreateUserRequest) (Response, error)
func mapHandler(c *gin.Context, request map[string]int) (Response, error)
```

Interfaces sao rejeitadas porque nao produzem um contrato documental util.

## Tipos de response suportados

A response deve ser um tipo concreto com representacao JSON padrao. Structs, valores escalares, slices e maps com chave string sao permitidos.

Ponteiros de response ainda sao rejeitados porque a fase atual nao possui uma policy explicita para decidir entre JSON `null`, status 204 ou erro quando o ponteiro e nil.

Slices e maps top-level nil tambem nao sao serializados como `null` quando o schema nao e nullable. O adapter produz status 500 e registra `jsonendpoint.ErrNilResponse`. Containers nil aninhados tambem produzem status 500 e registram um erro de validacao da response. Inicialize containers que devem aparecer na response:

```go
return ListUsersResponse{
	Users: []UserResponse{},
}, nil
```

Campos com `omitempty` podem continuar omitindo containers vazios ou nil.

## Restricoes dos DTOs

O adapter rejeita no registro tipos cuja representacao produzida por `encoding/json` nao pode ser descrita com fidelidade pelo reflector atual. Entre os casos rejeitados estao:

- `json.RawMessage`;
- `json.Number`;
- `[]byte` e outras sequencias de bytes;
- interfaces;
- maps com chave diferente de string;
- containers recursivos;
- custom `MarshalJSON`, `UnmarshalJSON`, `MarshalText` ou `UnmarshalText`;
- campos embedded sem tag `json` explicita;
- campos com `json:",string"`;
- campos diferentes com o mesmo nome JSON;
- kinds sem representacao suportada, como `func`, `chan`, `complex` e `uintptr`.

Configuracoes invalidas causam panic durante o registro com prefixo `jsonendpoint:`. Isso faz o erro aparecer durante a inicializacao da aplicacao, antes de atender requests.

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
	DocumentationMode: routekit.DocumentAll,
})
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
	UseDocumented(rateLimitMiddleware, routekit.MiddlewareMetadata{
		Responses: []routekit.DocResponse{
			{Status: http.StatusTooManyRequests, Description: "Too Many Requests"},
		},
	})
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
