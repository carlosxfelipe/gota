# 🛡️ SECURITY TODO — Hardening de Endpoints, Docs e TypeScript SDK

Este checklist reúne os pontos de auditoria e hardening identificados para mitigar vetores de ataque comuns explorados por equipes de teste de intrusão (*penetration testing / red teaming*), com foco nas rotas de documentação (`/docs`, `/openapi.json`), geração dinâmica de TypeScript (`/types.d.ts`, `/sdk.ts`) e mecanismos de autenticação (`app.UseDocs`, `BasicAuth`).

---

## 🚨 Prioridade Alta (Vazamento de Informação & Controle de Acesso)

- [x] **1. Header de `Cache-Control` anti-vazamento nas rotas de Docs e SDK**
  - **Cenário de Ataque**: CDNs (Cloudflare, CloudFront) e proxies reversos (Nginx) costumam cachear arquivos estáticos (`.ts`, `.js`, `.json`). Se um usuário autenticado acessar `/sdk.ts` ou `/types.d.ts`, a CDN pode cachear a resposta e servi-la para requisições anônimas subsequentes, contornando a autenticação.
  - **Ação no Framework**: Definido automaticamente headers restritivos em `specRoute`, `docsRoute`, `tsRoute` e `sdkRoute`:
    ```http
    Cache-Control: no-store, no-cache, must-revalidate, private
    Pragma: no-cache
    ```
  - **Critério de Aceite**: Implementado em `core/app.go` (`setDocsNoCacheHeaders`) com suporte a override customizado e coberto por testes em `core/docs_security_test.go` e `core/typescript_test.go`.

- [x] **2. Rate Limiting obrigatório/acoplado para `UseDocs` / `BasicAuth`**
  - **Cenário de Ataque**: `BasicAuth` com apenas usuário e senha é vulnerável a ataques automatizados de força bruta e *credential stuffing* (usando ferramentas como `ffuf`, `hydra`, `Burp Intruder`), testando dezenas de milhares de senhas por minuto.
  - **Ação**:
    - Suporte nativo a `RateLimit *RateLimiterOptions` em `BasicAuthOptions` para acoplamento direto em qualquer rota/grupo protegido por BasicAuth.
    - Suporte declarativo a `DocsRateLimit *RateLimiterOptions` em `gota.Config`.
    - Suporte fluente via `app.UseDocs(gota.RateLimiter(...), gota.BasicAuth(...))`.
  - **Critério de Aceite**: Implementado em `core/middleware.go` e `core/app.go` com testes unitários em `core/middleware_test.go` e `core/docs_security_test.go` garantindo bloqueio HTTP 429 com `Retry-After` em ataques de força bruta.

- [x] **3. Desativação segura das rotas de TypeScript / SDK em Produção**
  - **Cenário de Ataque**: Navegadores em produção não executam `.ts` nativamente. Deixar `/types.d.ts` e `/sdk.ts` expostos via HTTP em produção serve apenas para atacantes mapearem 100% da sua API sem precisar de esforço de reconhecimento.
  - **Ação**:
    - Implementados métodos de exportação offline em disco `app.ExportTypeScript(path)` e `app.ExportTypeScriptSDK(path)` com criação automática de diretórios-pai.
    - Criado exemplo funcional de geração offline em `examples/codegen/main.go` para pipelines de CI/CD.
    - Implementado aviso no log em `app.Listen` alertando quando `ENV=production` ou `APP_ENV=production` e rotas de documentação ou TypeScript estiverem ativas sem proteção.
  - **Critério de Aceite**: Testes em `core/typescript_test.go` (`TestTypeScript_ExportOfflineFiles`) e `core/app_test.go` (`TestIsProductionEnv`), com documentação sincronizada em `README.md` e `llms.txt`.

---

## ⚠️ Prioridade Média (Modelos, Isolamento & Credenciais)

- [ ] **4. Auditoria de Schemas TypeScript contra Mass Assignment e Vazamento de Dados**
  - **Cenário de Ataque**: Structs de domínio/banco sem `json:"-"` expõem propriedades internas (ex: `password_hash`, `role`, `is_admin`, `reset_token`, `stripe_id`) no `.d.ts`. O pentester usa isso para tentar injeção de parâmetros (*Mass Assignment*) nos endpoints de criação/atualização.
  - **Ação**:
    - Adicionar no `TypeScriptGenerator` detecção ou *warning* durante testes quando structs expõem campos sensíveis conhecidos (ex: que contenham `password`, `secret`, `hash`, `token`, `salt`) sem a tag `json:"-"`.
    - Reforçar na documentação o uso de DTOs separados para Request e Response.

- [ ] **5. Alerta contra Credenciais Hardcoded e Senhas Fracas no `BasicAuth`**
  - **Cenário de Ataque**: Senhas literais no código (ex: `BasicAuth("admin", "admin123")`) vão para o histórico do Git e ficam embutidas em texto claro dentro do binário Go compilado (`strings binary | grep admin`).
  - **Ação**:
    - No `gota.BasicAuth()`, emitir um `log.Printf("⚠️ ALERTA DE SEGURANÇA: ...")` se forem detectadas senhas comuns/fracas (`"admin"`, `"admin123"`, `"123456"`, `"password"`, `"secret"`, `"root"`).
    - Recomendar fortemente nas docs e exemplos o uso exclusivo de `os.Getenv("DOCS_PASSWORD")`.

- [ ] **6. Isolamento de CORS nas Rotas de Documentação e Schemas**
  - **Cenário de Ataque**: Se o CORS global estiver configurado permissivamente, requisições autenticadas via browser (onde o usuário tem o BasicAuth memorizado) poderiam teoricamente ser lidas por sites terceiros maliciosos se as origens forem refletidas.
  - **Ação**:
    - Garantir que as rotas de documentação (`docsRoutes`) rejeitem requisições CORS de origens cruzadas não confiáveis ou que não herdem configurações CORS frouxas sem validação explícita.

- [ ] **7. Ocultação em Lote de Rotas Internas / Administrativas**
  - **Cenário de Ataque**: Esquecer de adicionar `.Hidden()` em uma rota interna ou administrativa faz com que ela seja incluída automaticamente no `/types.d.ts` e `/sdk.ts`.
  - **Ação**:
    - Permitir aplicar `.Hidden()` a grupos inteiros de rotas (`Group.Hidden()`), garantindo que subárvores inteiras (ex: `/internal/*`, `/admin/*`) fiquem fora da documentação e do SDK público por padrão.

---

## 🔍 Boas Práticas & Hardening Operacional

- [ ] **8. Validação de Transporte Seguro (HTTPS Obrigatório para BasicAuth)**
  - **Cenário de Ataque**: Em conexões HTTP sem TLS, o cabeçalho `Authorization: Basic <base64>` trafega em texto claro na rede local / Wi-Fi / proxies, permitindo captura imediata por farejadores de pacotes (Wireshark / tcpdump).
  - **Ação**:
    - No middleware `BasicAuth`, se a requisição não for de `localhost`/`127.0.0.1` e não possuir TLS (`ctx.Request.TLS == nil` e cabeçalhos de proxy como `X-Forwarded-Proto != "https"`), emitir aviso de segurança ou rejeitar a requisição com status `403 Forbidden` caso uma opção `RequireTLS` esteja ativada.

- [ ] **9. Alternativa de Autenticação com Expiração (Token / IP Whitelist)**
  - **Cenário de Ataque**: O HTTP Basic Auth não possui mecanismo de logout nem expiração de sessão; o navegador guarda e reenvia a credencial indefinidamente.
  - **Ação**:
    - Documentar e fornecer helpers no `app.UseDocs` para proteção baseada em lista de IPs confiáveis (IP Whitelisting / VPN) e autenticação temporária (Bearer token / JWT curto de desenvolvedor).

