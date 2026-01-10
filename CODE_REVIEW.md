# Code Review - nazim

## 🔴 Problemas Críticos

### 1. **Windows Install - Lógica de Intervalo/Startup Incorreta**
**Arquivo:** `internal/platform/windows.go:169-195`

**Problema:** A lógica de criação de tarefas está sobrescrevendo `args` em vez de construir corretamente. Se um serviço tem `OnStartup=true` E `Interval > 0`, apenas o intervalo é considerado.

```go
// PROBLEMA: Se tiver intervalo, sobrescreve args completamente
if svc.GetInterval() > 0 {
    args = []string{...}  // Perde WorkDir e outras configurações
}

// PROBLEMA: Se for startup, também sobrescreve
if svc.OnStartup && svc.GetInterval() == 0 {
    args = []string{...}  // Perde WorkDir novamente
}
```

**Impacto:** Serviços com startup + intervalo não funcionam corretamente. WorkDir é perdido.

**Solução:** Construir args incrementalmente ou usar uma função auxiliar.

### 2. **Darwin Install - Parsing de Comando Incorreto**
**Arquivo:** `internal/platform/darwin.go:43`

**Problema:** `strings.Fields(svc.Command)` quebra comandos com espaços em múltiplos argumentos.

```go
parts := strings.Fields(svc.Command)  // "python script.py" vira ["python", "script.py"]
```

**Impacto:** Comandos como `"python script.py"` são divididos incorretamente. O comando já está separado em `svc.Command` e `svc.Args`.

**Solução:** Usar apenas `svc.Command` como primeiro elemento do array, não fazer split.

### 3. **Race Condition no Notepad Monitoring**
**Arquivo:** `internal/cli/cli.go:488-521`

**Problema:** Goroutine pode continuar rodando após o processo principal terminar.

**Impacto:** Potencial leak de goroutine.

**Solução:** Usar context para cancelar a goroutine.

### 4. **Injection de Comando no Windows**
**Arquivo:** `internal/platform/windows.go:152`

**Problema:** `command` é construído com `strings.Join` sem sanitização.

```go
command := strings.Join(cmdParts, " ")  // Vulnerável a injection
```

**Impacto:** Se `svc.Command` ou `svc.Args` contiverem caracteres especiais do Windows, pode causar execução de comandos não intencionais.

**Solução:** Escapar caracteres especiais ou usar array de argumentos separados.

## ⚠️ Problemas Moderados

### 5. **Tratamento de Erros Inconsistente**
**Arquivo:** Vários

**Problemas:**
- `_ = cmd.Run()` ignora erros silenciosamente em vários lugares
- `IsInstalled` retorna `false, nil` mesmo para erros de permissão
- Erros são ignorados com `_` sem log

**Impacto:** Dificulta debugging e pode mascarar problemas reais.

### 6. **Validação de Nome de Serviço Fraca**
**Arquivo:** `internal/service/service.go:94-104`

**Problema:** Não valida caracteres inválidos no nome (espaços, caracteres especiais que podem quebrar Task Scheduler/systemd/launchd).

**Impacto:** Nomes inválidos podem causar falhas silenciosas.

### 7. **Parse Duration - Aceita Valores Negativos**
**Arquivo:** `internal/cli/cli.go:257`, `internal/service/service.go:74`

**Problema:** `fmt.Sscanf("%d")` aceita números negativos sem validação.

**Impacto:** Intervalos negativos podem ser aceitos.

### 8. **Windows - WorkDir Não Implementado**
**Arquivo:** `internal/platform/windows.go:163-167`

**Problema:** Comentário diz que `/cwd` não é suportado, mas não há implementação alternativa.

**Impacto:** WorkDir é ignorado no Windows.

### 9. **Linux - Não Verifica Erros de systemctl**
**Arquivo:** `internal/platform/linux.go:86-88, 97-98`

**Problema:** `_ = exec.Command(...).Run()` ignora erros de daemon-reload e enable.

**Impacto:** Falhas silenciosas na instalação.

### 10. **Darwin - Parsing de Comando Duplicado**
**Arquivo:** `internal/platform/darwin.go:42-49`

**Problema:** Faz split do comando E adiciona args, causando duplicação se o comando já tiver argumentos.

## 💡 Melhorias Sugeridas

### 11. **Context não é usado**
**Arquivo:** `internal/cli/cli.go`

**Problema:** Context é passado mas nunca usado para cancelamento.

**Solução:** Usar context para cancelar operações longas.

### 12. **Falta de Timeout em Comandos**
**Arquivo:** Vários `exec.Command`

**Problema:** Comandos podem travar indefinidamente.

**Solução:** Adicionar timeouts usando context.

### 13. **Logging Inconsistente**
**Problema:** Mistura de `fmt.Printf`, `fmt.Fprintf(os.Stderr)`, e ausência de logs estruturados.

**Solução:** Usar biblioteca de logging ou pelo menos padronizar.

### 14. **Testes Unitários Ausentes**
**Problema:** Não há testes para funções críticas.

**Solução:** Adicionar testes, especialmente para parsing e validação.

### 15. **Documentação de Funções**
**Problema:** Algumas funções não têm documentação adequada.

**Solução:** Adicionar godoc comments.

## 📋 Resumo de Prioridades

**Alta Prioridade:**
1. Corrigir lógica de Install no Windows (intervalo + startup)
2. Corrigir parsing de comando no Darwin
3. Sanitizar comandos no Windows
4. Validar nomes de serviços

**Média Prioridade:**
5. Melhorar tratamento de erros
6. Implementar WorkDir no Windows
7. Adicionar timeouts
8. Validar intervalos negativos

**Baixa Prioridade:**
9. Usar context adequadamente
10. Melhorar logging
11. Adicionar testes
12. Melhorar documentação
