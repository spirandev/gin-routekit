# Migracao para OpenAPI 3.1 e diagnosticos de fidelidade

Esta entrega altera o contrato de geracao OpenAPI e a semantica do pacote `jsonendpoint`. Nao ha defaults ocultos para preservar documentos ambiguos: ajuste a configuracao e os contratos antes de publicar o novo documento.

## Resumo das mudancas incompativeis

- o documento passa de OpenAPI 3.0.3 para OpenAPI 3.1.0, com o dialect base da OAS 3.1 em `jsonSchemaDialect`, fundamentado no JSON Schema 2020-12;
- `OpenAPIConfig.BasePath` e `OpenAPIConfig.PathMode` passam a ser obrigatorios;
- operacoes sem response deixam de receber `200 OK` implicito e agora sao invalidas;
- schemas passam a ser direcionais, com required, nulabilidade, fields e codecs diferentes para request e response quando necessario;
- codecs JSON arbitrarios exigem schema explicito, registration ou provider;
- `jsonendpoint` passa a seguir `encoding/json` para `null` e valores nil;
- validacoes OpenAPI passam a agregar erros e warnings, incluindo conflitos de profiles e security;
- `DocProfile.Description` passa a aparecer em `x-routekit-profiles` quando o profile e usado.

## Configurar BasePath e PathMode

Antes, a configuracao podia omitir a politica de paths:

```go
config := routekit.OpenAPIConfig{
	Title:   "Users API",
	Version: "1.0.0",
}
```

Agora escolha explicitamente uma das duas representacoes.

Para paths relativos ao base path:

```go
config := routekit.OpenAPIConfig{
	Title:    "Users API",
	Version:  "1.0.0",
	BasePath: "/api",
	PathMode: routekit.PathsRelativeToBase,
}
```

Uma rota Gin registrada como `/api/users` e emitida como `/users`. Sem `Servers`, o documento recebe `servers: [{"url":"/api"}]`. Se `Servers` for informado, o path da URL de cada server deve conter `/api` como prefixo por segmento.

Para preservar paths registrados completos:

```go
config := routekit.OpenAPIConfig{
	Title:    "Users API",
	Version:  "1.0.0",
	BasePath: "/api",
	PathMode: routekit.FullRegisteredPaths,
	Servers: []routekit.OpenAPIServer{
		{URL: "https://api.example.com"},
	},
}
```

Nesse modo, `/api/users` permanece no documento. O `Server.URL` nao pode repetir `/api`, pois isso produziria `/api/api/users` para consumidores OpenAPI.

`BasePath` deve ser absoluto e normalizado, sem query, fragment ou trailing slash, exceto `/`.

## Declarar todas as responses

Antes, uma operacao sem responses recebia uma resposta `200 OK` sem body. Esse fallback foi removido.

Antes:

```go
group.GET("/health", health, "Health", 1).Document()
```

Depois, declare o comportamento real:

```go
group.GET("/health", health, "Health", 1).
	Document().
	Contract(routekit.JSONResponseContractOf[HealthResponse](200, "OK"))
```

Para sucesso sem body:

```go
group.DELETE("/sessions/:id", deleteSession, "Delete session", 2).
	Document().
	Contract(routekit.EmptyJSONResponseContract(
		204,
		"No Content",
		routekit.WithContractParameter(routekit.DocParam{
			Name: "id", In: routekit.DocParamInPath,
			Type: "string", Required: true,
		}),
	))
```

Uma operacao sem qualquer response e erro. Uma operacao com responses, mas sem status 2xx, gera warning. Status 204 e 205 com schema de body sao invalidos.

## Migrar contratos genericos

`JSONContractOf[Request, Response]` continua disponivel como alias deprecated, mas codigo novo deve explicitar a categoria do contrato.

Request e response:

```go
route.Contract(
	routekit.JSONRequestContractOf[CreateUserRequest, CreateUserResponse](
		http.StatusCreated,
		"Created",
	),
)
```

Somente response:

```go
route.Contract(
	routekit.JSONResponseContractOf[ListUsersResponse](http.StatusOK, "OK"),
)
```

Sem request e sem response body:

```go
route.Contract(
	routekit.EmptyJSONResponseContract(http.StatusNoContent, "No Content"),
)
```

Para respostas adicionais e defaults JSON tipados:

```go
unauthorized := routekit.ResponseOf[ErrorResponse](
	http.StatusUnauthorized,
	"Unauthorized",
)

contract := routekit.JSONResponseContractOf[ListUsersResponse](
	http.StatusOK,
	"OK",
	routekit.WithAdditionalResponse(unauthorized),
)

config.Defaults = routekit.WithJSONDefaults(
	routekit.DefaultResponseOf[ErrorResponse](
		http.StatusInternalServerError,
		"Internal Server Error",
	),
)
```

Options especificas de request em `JSONResponseContractOf` ou `EmptyJSONResponseContract`, e options de body de response em `EmptyJSONResponseContract`, sao reportadas como configuracao incoerente.

## Revisar schemas direcionais

OpenAPI 3.1 representa nulabilidade com JSON Schema, usando `null` em composicoes como `anyOf`; o keyword OpenAPI 3.0 `nullable` nao e mais emitido.

O mesmo tipo Go agora e analisado separadamente:

- request considera fields aceitos pelo decoder, `binding:"required"`, `UnmarshalText`, `UnmarshalJSON` e chaves de map decodificaveis;
- response considera fields emitidos, `omitempty`, `omitzero`, promocao de embedded fields, `MarshalText`, `MarshalJSON` e chaves de map serializaveis;
- ponteiros, interfaces, maps e slices recebem nulabilidade conforme o comportamento da direcao;
- `json:",string"`, `[]byte`, `json.RawMessage`, `json.Number`, aliases, generics e recursao sao refletidos sem executar valores.

Schemas canonicos equivalentes podem compartilhar o mesmo component. Quando request e response diferem, recebem components separados. Nomes de refs podem mudar; snapshots nao devem depender dos nomes antigos sem revisao.

Um schema manual em `DocBody.Schema` ou `DocResponse.Schema` continua sendo a forma de maior controle:

```go
route.Body(routekit.OpenAPISchema{
	Type: "object",
	Properties: map[string]routekit.OpenAPISchema{
		"value": {Type: "string"},
	},
})
```

## Declarar nomes, overrides e providers

Use `SchemaRegistrations` para associar configuracao a um tipo e, opcionalmente, a uma direcao:

```go
config.SchemaRegistrations = []routekit.SchemaRegistration{
	routekit.RegisterSchemaAs[CreateUserRequest](
		"CreateUserInput",
		routekit.ForSchemaRequest(),
	),
	routekit.RegisterSchemaAs[CreateUserResponse](
		"CreateUserOutput",
		routekit.ForSchemaResponse(),
	),
	routekit.OverrideSchemaOf[ExternalDate](
		routekit.OpenAPISchema{Type: "string", Format: "date"},
		routekit.ForSchemaDirections(
			routekit.SchemaRequest,
			routekit.SchemaResponse,
		),
	),
}
```

Sem option de direcao, o registro vale para request e response. `Components.Schemas` continua servindo para components independentes que nao estao associados a um tipo Go.

Para tipos que controlam seu proprio contrato, implemente um provider comum:

```go
func (ExternalDate) OpenAPISchema() routekit.OpenAPISchema {
	return routekit.OpenAPISchema{Type: "string", Format: "date"}
}
```

Ou um provider direcional:

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

Pointer receivers tambem sao aceitos e recebem uma nova instancia zero. Panic, schema invalido, registro duplicado ou colisao de nome gera diagnostico. Um codec `MarshalJSON`/`UnmarshalJSON` arbitrario sem schema, override ou provider nao recebe fallback inventado.

## Migrar null e nil no jsonendpoint

No spike anterior, JSON `null`, pointer response nil e containers nil podiam ser rejeitados. Agora o runtime segue `encoding/json`.

Body obrigatorio continua rejeitando ausencia:

```text
body ausente  -> 400 invalid request
body `null`   -> valor presente, decodificado por encoding/json
```

Para um request `*CreateUserRequest`, `null` entrega `nil` ao handler. Para uma struct, `null` mantem o zero value. Para fields escalares nao ponteiro, `null` tambem mantem o valor existente/zero. Tags `binding:"required"` e `WithValidator` rodam depois do decode e podem rejeitar esse resultado.

`WithOptionalBody` permite ausencia e entrega o zero value, inclusive `nil` para ponteiros, slices e maps. Ele nao altera a semantica de um body presente.

Responses nil agora serializam como `null`:

```go
func handler(
	c *gin.Context,
	request ListRequest,
) ([]Item, error) {
	return nil, nil // HTTP 200 com body JSON null
}
```

O mesmo vale para ponteiros e containers nil aninhados. Inicialize `[]Item{}` ou `map[string]Item{}` quando o contrato de negocio exigir array ou objeto vazio. Status 204 e 205 continuam descartando o valor retornado e nunca escrevem body.

Tipos com codecs customizados podem executar normalmente no adapter, mas o build OpenAPI exige provider, override ou schema explicito. Erros reais de marshal/unmarshal continuam usando as policies 400/500 e sao anexados a `gin.Context.Errors`.

## Migrar middleware documentado e seguranca

`UseDocumented` agora aceita contribuicoes variadicas. Literals existentes de `MiddlewareMetadata` continuam validos, pois o tipo implementa `DocumentationContribution`.

Antes:

```go
group.UseDocumented(authMiddleware, routekit.MiddlewareMetadata{
	Responses: []routekit.DocResponse{
		{Status: 401, Description: "Unauthorized"},
	},
})
```

Depois:

```go
group.UseDocumented(
	authMiddleware,
	routekit.RequireSecurityScheme("BearerAuth"),
	routekit.RespondsWith(
		routekit.ResponseOf[ErrorResponse](401, "Unauthorized"),
	),
)
```

Contribuicoes disponiveis incluem `RequireSecurityScheme`, `RequireOperationHeader`, `RequiresHeader`, `RequiresSecurity`, `RespondsWith` e `UsesProfiles`. Elas nao inspecionam o codigo do middleware.

Security scheme e operation header devem permanecer separados. Para API key em header:

```go
config.Components.SecuritySchemes = map[string]*routekit.OpenAPISecurityScheme{
	"APIKey": routekit.APIKeyHeaderSecurityScheme("X-API-Key", "API key"),
}
```

`RequireSecurityScheme("APIKey")` adiciona somente o security requirement. Nao adicione tambem `RequiresHeader("X-API-Key", ...)` para a mesma operacao, pois a duplicacao e um erro.

Regras podem derivar documentacao das flags exportadas do handler:

```go
config.SecurityRules = []routekit.RouteSecurityRule{
	routekit.WhenAuthenticated(routekit.RequireSecurityScheme("BearerAuth")),
	routekit.WhenM2M(routekit.RequireSecurityScheme("ClientCredentials")),
	routekit.WhenIntegration(routekit.UsesProfiles("integration")),
}
```

Use `NewRouteSecurityRule` para predicado customizado. Requirements sem scheme definido, scopes em tipos incompativeis e header de API key duplicado sao erros. Scheme declarado e nao usado gera warning.

## Expor descriptions de profiles

Adicione `Description` aos profiles que precisam ser descobertos por tooling:

```go
config.Profiles = map[string]routekit.DocProfile{
	"tenant": {
		Description: "Requests scoped to one tenant",
		Headers: []routekit.DocParam{{
			Name: "X-Tenant",
			In: routekit.DocParamInHeader,
			Type: "string",
			Required: true,
		}},
	},
}
```

Somente profiles efetivamente usados aparecem em `x-routekit-profiles`. Profile desconhecido e conflito de parametros entre profiles sao erros; profile repetido gera warning.

## Adotar Validate, Build e Marshal

Antes de publicar, valide e inspecione o relatorio completo:

```go
report, err := routekit.ValidateOpenAPI(routes, config)
for _, diagnostic := range report.Diagnostics {
	log.Printf(
		"%s %s %s %s: %s",
		diagnostic.Severity,
		diagnostic.Code,
		diagnostic.Method,
		diagnostic.Path,
		diagnostic.Message,
	)
}
if err != nil {
	return err
}

document, err := routekit.BuildOpenAPI(routes, config)
if err != nil {
	return err
}
payload, err := routekit.MarshalOpenAPI(document)
```

`ValidateOpenAPI` retorna o `DiagnosticReport` e um `*DiagnosticsError` quando existe pelo menos um erro; `errors.As` pode recuperar o mesmo report do erro. Warnings nao bloqueiam `BuildOpenAPI`. `MarshalOpenAPI` serializa o documento de forma deterministica e indentada; nao registra rota HTTP.

Com `AppRouter`, registre primeiro o snapshot de rotas:

```go
if err := appRouter.RegisterRoutes(engine); err != nil {
	return err
}
report, err := appRouter.ValidateOpenAPI(config)
document, err := appRouter.BuildOpenAPI(config)
err = appRouter.RegisterOpenAPI(engine, config)
```

Os dois primeiros metodos nao registram endpoint. `RegisterOpenAPI` usa o mesmo build/marshal e registra `config.JSONPath`, ou `/openapi.json` quando vazio.
