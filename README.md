# Freecompute — Computação Voluntária Descentralizada (MVP)

O **Freecompute** é uma plataforma de computação voluntária (*volunteer computing*) descentralizada e verificável construída em **Go (>= 1.22)**. Três ou mais nós coordenam a execução de tarefas computacionais determinísticas isoladas em containers através de:

- **Freechains**: camada de consenso P2P pub-sub e sistema de reputação (*reps*) com chaves `ed25519` e votos (`like` / `dislike`).
- **IPFS (Kubo)**: armazenamento endereçado por conteúdo (CIDs) para os dados de entrada (*inputs*) e diretórios de saída (*outputs*) das tarefas.
- **Podman (Rootless)**: sandbox de execução isolada de containers (sem rede, filesystem raiz *read-only*, todas as *capabilities* Linux removidas, limites de CPU/memória/PIDs/tmpfs e imagem fixada por digest SHA-256).
- **Ergo (`ergo.services/ergo`)**: modelo de atores (*actor model*) para concorrência segura sem compartilhamento de memória ou locks.
- **Protocol Buffers (`vc/v1`)**: serialização determinística das mensagens do protocolo na cadeia (`Envelope`, `JobAnnounce`, `ResultCommit`, `ResultReveal`, `JobFinalize`).

---

## Sumário

1. [Arquitetura do Projeto](#arquitetura-do-projeto)
2. [O Protocolo Commit-Reveal e Quórum](#o-protocolo-commit-reveal-e-quórum)
3. [Requisitos e Pré-requisitos](#requisitos-e-pré-requisitos)
4. [Preparação do Ambiente](#preparação-do-ambiente)
5. [Como Compilar e Executar os Testes](#como-compilar-e-executar-os-testes)
6. [Como Executar a Demo (`make demo` e `make demo-divergent`)](#como-executar-a-demo-make-demo-e-make-demo-divergent)
7. [Como a Demo Funciona Passo a Passo](#como-a-demo-funciona-passo-a-passo)
8. [Uso Direto da CLI (`vcnode`)](#uso-direto-da-cli-vcnode)
9. [Concessões do MVP e Premissas Técnicas](#concessões-do-mvp-e-premissas-técnicas)
10. [Implementação](#implementação)

---

## Arquitetura do Projeto

```text
proto/vc/v1/envelope.proto   # Definições Protobuf: Envelope, JobAnnounce, ResultCommit, ResultReveal, JobFinalize
vc/v1/envelope.pb.go         # Código Go gerado a partir do Protobuf
internal/protocol/           # Lógica pura e determinística do protocolo (JobID, Commitment, Replay, Tally)
internal/actors/             # Portas, adaptadores e atores Ergo (ChainBridge, WorkerActor, SubmitterActor)
internal/ipfs/               # Cliente HTTP para o daemon Kubo (Add, AddDir, Cat, Fetch, Pin, Unpin, Ping)
internal/sandbox/            # Executor de containers isolados via socket do Podman v5
internal/canon/              # Digest canônico determinístico SHA-256 de diretórios (DirectoryDigest)
freechains/                  # Wrapper Go sobre o CLI do Freechains (os/exec sem shell, serializado)
cmd/vcnode/main.go           # Ponto de entrada de um nó (Submitter + Worker ou apenas Worker)
scripts/demo.sh              # Script de demonstração fim a fim com 3 nós (alice, bob, carol)
```

### Componentes em `internal/actors`

Cada processo `vcnode` inicia um nó Ergo local com os seguintes atores:

- **`ChainBridge`**: único componente que interage com o Freechains. Em um timer periódico:
  1. Executa `SyncRecv` e `SyncSend` com todos os pares configurados (`--peers`).
  2. Consulta a ordem consensual da cadeia (`ListOrder`).
  3. Busca metadados (`GetMetadata`) e payloads (`GetPayload`) de novos posts, autenticando o autor exclusivamente pelo campo `sign` dos metadados da cadeia.
  4. Decodifica os `Envelope`s Protobuf e transmite um `ChainUpdate` ordenado para os atores locais (`WorkerActor` e `SubmitterActor`).
  5. Processa chamadas síncronas de publicação (`PublishReq`) e votação (`VoteReq`).
- **`WorkerActor`** (ativo em todos os nós): a cada `ChainUpdate`, reconstrói o estado da cadeia via `protocol.Replay(entries)`.
  - Na fase `Committing`, se ainda não comitou para o job anunciado, baixa o input do IPFS, executa o container no Podman, calcula o hash canônico do diretório `/output` (`canon.DirectoryDigest`) e publica `ResultCommit`.
  - Na fase `Revealing`, se seu commit está entre os $N$ primeiros commits válidos contabilizados, publica o diretório `/output` no IPFS (`AddDir`) e posta `ResultReveal`.
  - Suporta dois modos via `--behavior`: `honest` (padrão) ou `divergent` (sobrescreve o resultado com `999` antes de calcular o hash para simular um trabalhador malicioso/defeituoso).
- **`SubmitterActor`** (ativo apenas quando `--submit=<arquivo>` é informado):
  - Faz upload do arquivo de entrada no IPFS e publica `JobAnnounce`.
  - Monitora a evolução do estado via `protocol.Replay(entries)`.
  - Quando todos os trabalhadores contabilizados revelam (ou quando o quórum $Q$ é atingido e o timer local `--grace` expira), verifica o resultado vencedor baixando-o do IPFS e recalculando `canon.DirectoryDigest`.
  - Publica `JobFinalize` e emite votos de reputação no Freechains: `like` para cada vencedor e `dislike` para cada trabalhador divergente ou que não revelou.

---

## O Protocolo Commit-Reveal e Quórum

Parâmetros do MVP: **N = 3 réplicas**, **Q = 2 quórum**. Todos os marcos do protocolo baseiam-se na **posição das mensagens na ordem consensual da cadeia** (`list order`), nunca em relógio de parede (*wall-clock*).

1. **Anúncio (`JobAnnounce`)**: O Submitter adiciona o input ao IPFS e posta `JobAnnounce{input_cid, image_ref, cmd, replicas=3, quorum=2, reward_reps=1000}`. O identificador do job (`job_id`) é o `SHA-256` da serialização determinística do `JobAnnounce`.
2. **Fase de Commit (`ResultCommit`)**: Cada Worker executa a tarefa e posta `ResultCommit{commitment}`
   O `worker_id` é sempre a chave pública autenticada pelo Freechains nos metadados da ação (`sign`), impedindo falsificação de identidade. Commits duplicados do mesmo autor são ignorados (vale apenas o primeiro).
3. **Transição para Reveal**: Os primeiros N = 3 commits válidos na ordem da cadeia são contabilizados. Assim que o N-ésimo commit válido aparece na cadeia, a fase muda deterministicamente de `Committing` para `Revealing` (commits posteriores são ignorados).
4. **Fase de Reveal (`ResultReveal`)**: Cada Worker cujo commit foi contabilizado envia seu diretório `/output` ao IPFS e publica `ResultReveal{result_hash, result_cid, nonce}`. Um reveal só é válido se pertencer a um trabalhador contabilizado e se o `Commitment` recalculado bater exatamente com o commit original daquele autor.
5. **Apuração (`Tally`) e Finalização (`JobFinalize`)**:
   - Os reveals válidos são agrupados por `result_hash`.
   - Um grupo com pelo menos Q = 2 membros vence (`HasQuorum = true`, `AcceptedHash = result_hash`).
   - Trabalhadores cujo `result_hash` coincide com `AcceptedHash` são classificados como `Winners`; os que revelaram hash diferente são `Divergent`; os que comitaram mas não revelaram são `NoReveal`.
   - O Submitter baixa do IPFS o `result_cid` de um vencedor, confere se `canon.DirectoryDigest` é igual a `AcceptedHash`, posta `JobFinalize` e vota na cadeia Freechains (`like <reward> author <pub>` para cada vencedor e `dislike <reward> author <pub>` para cada `Divergent` ou `NoReveal`).

---

## Requisitos e Pré-requisitos

### Dependências de Sistema
- **Linux** (com suporte a user namespaces para Podman rootless)
- **Go** `>= 1.22`
- **Freechains** CLI (`freechains` v0.20 no `PATH`) + Lua 5.4 (`lua` ou `lua5.4`) + Git (`git` e `git-daemon`)
- **OpenSSH** (`ssh-keygen` no `PATH`, usado para gerar e assinar com chaves `ed25519`)
- **IPFS (Kubo)** (`ipfs` no `PATH`)
- **Podman** (`podman` v5 no `PATH`, configurado em modo rootless)
- **cURL** (`curl`, utilizado pelo `demo.sh` para verificar a API do Kubo)

---

## Preparação do Ambiente

Antes de rodar a demo ou o `vcnode`, certifique-se de que os serviços do **IPFS (Kubo)** e do **Podman rootless** estejam ativos e que a imagem do container esteja disponível localmente.

### 1. Iniciar o daemon do IPFS (Kubo)

Caso ainda não tenha inicializado o repositório IPFS local:
```bash
ipfs init
```
Em um terminal separado (ou em background), inicie o daemon:
```bash
ipfs daemon
```
> Por padrão, o `vcnode` utiliza `http://127.0.0.1:5001` (configurável via variável de ambiente `VC_IPFS_API`).

### 2. Iniciar o socket da API do Podman (Rootless)

Ative o socket de usuário via `systemd` ou inicie o serviço diretamente:
```bash
systemctl --user start podman.socket
# OU em um terminal dedicado:
podman system service --time=0
```
> Por padrão, o `vcnode` conecta em `unix://$XDG_RUNTIME_DIR/podman/podman.sock` (ou `/run/user/$(id -u)/podman/podman.sock`, configurável via `VC_PODMAN_SOCKET`).

### 3. Obter a imagem do container fixada por digest (`VC_IMAGE`)

O protocolo exige que a imagem seja fixada por digest SHA-256 (`<repositório>@sha256:<64-hex>`), sem tag `:3.20` antes do `@sha256:`. Para baixar o `alpine:3.20` e extrair seu digest real:

```bash
podman pull alpine:3.20
export VC_IMAGE="$(podman image inspect alpine:3.20 --format '{{index .RepoDigests 0}}')"
echo "$VC_IMAGE"
# Exemplo: docker.io/library/alpine@sha256:c64c687cbea9300178b30c95835354e34c4e4febc4badfe27102879de0483b5e
```

---

## Como Compilar e Executar os Testes

O `Makefile` na raiz do projeto automatiza a compilação, formatação e testes (incluindo as *build tags* necessárias do Podman):

```bash
# Compilar o binário em bin/vcnode
make build

# Executar todos os testes unitários (protocol, freechains, ipfs, canon, sandbox)
# e os testes E2E em memória de 3 nós (cenários honesto e divergente)
make test

# Executar toda a suíte de testes com o detector de condições de corrida (-race)
make race

# Formatar e verificar estaticamente o código
make fmt
make vet
```

---

## Como Executar a Demo (`make demo` e `make demo-divergent`)

Com o Kubo e o socket do Podman ativos:

### Cenário 1: Execução Honesta (`make demo`)
Todos os três nós (`alice`, `bob` e `carol`) operam com `--behavior=honest`, calculam a contagem correta de palavras (`11`) e recebem `like`:

```bash
make demo
# ou diretamente:
./scripts/demo.sh
```

**Saída esperada (`make demo`):**
```text
==> Building vcnode...
==> Generating ed25519 keys for alice, bob, carol...
==> Starting three Freechains daemons (8331, 8332, 8333) with --hub...
==> Initializing #vc on alice with three pioneers and cloning to bob and carol...
    alice chain id: #f427339bf74ab1cc438538048355dc6cb9165ed0
    bob   chain id: #f427339bf74ab1cc438538048355dc6cb9165ed0
    carol chain id: #f427339bf74ab1cc438538048355dc6cb9165ed0
==> Reputation (before):
    alice: 16666
    bob:   16666
    carol: 16666
==> Starting worker nodes bob (honest) and carol (honest)...
==> Starting alice with --submit...

==> Chain Timeline (consensus order on alice):
    [0] 685845f0b87ec7b71d43f013f47e72e4fd0eccbf  kind=post    author=alice
    [1] 8abf49a97fd9649e8f510c3c5d7014ddb6e6638c  kind=post    author=alice
    [2] 80942b5e580a828036481de4668cb198e9f3aea5  kind=post    author=bob
    [3] c1b981bb70c59cb91d70671fb7661fb8b32dc320  kind=post    author=carol
    [4] b7d5215366165a1016b2125ed79bac85a0d5369a  kind=post    author=alice
    [5] 3c1e267b0cb9c31716c745e9267e23dfa4213b7f  kind=post    author=carol
    [6] bb24b94ffcb3b39764c18cd1ff1b56cc8de90fe9  kind=post    author=bob
    [7] f6a0439706e2d1e2ae0a8c44491b813225320a5b  kind=post    author=alice
    [8] b9028d54b9ab8c030f85f72b057baedbeb052c2d  kind=like    author=alice
    [9] e320c07d7c4234a3190da7e4eb961bbd07a69786  kind=like    author=alice
    [10] 9b25c5245b1a5d5eac31c8620bd2f6cf008a1a35  kind=like    author=alice

==> Submitter Report & Verdict:
JOB_ID=84bec9dfcb33f0426d1497933ad44f96473789a39ee16e89e97819b2b122a0a0
ACCEPTED_HASH=d3beb797b44ba88fdceea6cef75d697cfc0b8285dc682f706c42efbcaa756622
VERIFIED=true
RESULT=11
WINNERS=ssh-ed25519 AAAA...,ssh-ed25519 AAAA...,ssh-ed25519 AAAA...
DIVERGENT=
NO_REVEAL=
    WINNERS_ALIASES=alice,bob,carol
    DIVERGENT_ALIASES=

==> Reputation (after):
    alice: 14066 (before: 16666)
    bob:   17066 (before: 16666)
    carol: 17066 (before: 16666)

PASS
```

### Cenário 2: Execução Divergente (`make demo-divergent`)
`alice` e `bob` operam com `--behavior=honest`, enquanto `carol` roda com `--behavior=divergent` (adulterando sua saída para `999` antes do commit/reveal). `alice` e `bob` formam quórum ($Q=2$) e vencem; `carol` é detectada como divergente, recebe um `dislike` e perde reputação:

```bash
make demo-divergent
# ou diretamente:
./scripts/demo.sh --divergent
```

**Saída esperada (`make demo-divergent`):**
```text
==> Submitter Report & Verdict:
JOB_ID=84bec9dfcb33f0426d1497933ad44f96473789a39ee16e89e97819b2b122a0a0
ACCEPTED_HASH=d3beb797b44ba88fdceea6cef75d697cfc0b8285dc682f706c42efbcaa756622
VERIFIED=true
RESULT=11
WINNERS=ssh-ed25519 AAAA...,ssh-ed25519 AAAA...
DIVERGENT=ssh-ed25519 AAAA...
NO_REVEAL=
    WINNERS_ALIASES=bob,alice
    DIVERGENT_ALIASES=carol

==> Reputation (after):
    alice: 14066 (before: 16666)
    bob:   17566 (before: 16666)
    carol: 15266 (before: 16666)

PASS
```

---

## Como a Demo Funciona Passo a Passo

O script [`scripts/demo.sh`](scripts/demo.sh) executa todo o ciclo de vida em um diretório temporário isolado (`/tmp/vc-demo-XXXXXX`) com `trap cleanup EXIT INT TERM` para encerrar todos os processos e limpar arquivos ao sair:

1. **Verificação de Pré-requisitos**:
   - Confere se os binários `freechains`, `ipfs`, `podman`, `ssh-keygen` e `go` estão no `PATH`.
   - Valida se `VC_IMAGE` está definida e fixada por digest (`@sha256:`).
   - Verifica se a API HTTP do Kubo (`127.0.0.1:5001`) e o socket Unix do Podman estão respondendo.
2. **Geração de Identidades Criptográficas**:
   - Gera 3 pares de chaves `ed25519` sem senha (`alice`, `bob` e `carol`) usando `ssh-keygen`.
3. **Inicialização dos Daemons Freechains**:
   - Inicia 3 daemons independentes do Freechains nas portas `8331` (`alice`), `8332` (`bob`) e `8333` (`carol`), todos com `--hub` (habilitando `receive-pack` para receber `sync send`) e `--reuseaddr`.
4. **Criação da Cadeia `#vc` e Pioneiros**:
   - `alice` inicializa a cadeia `#vc` passando as 3 chaves públicas como pioneiras (`chains add '#vc' init --pioneer=...`). Os 50.000 pontos iniciais de reputação são divididos igualmente entre os 3 pioneiros (`16666` reps cada).
   - `bob` e `carol` clonam a gênese a partir do daemon da `alice` (`chains add '#vc' clone localhost:8331`), e o script verifica que os 3 IDs de cadeia (`#<genesis-hash>`) são idênticos.
5. **Execução do Job (`wc -w`)**:
   - O script gera um arquivo de texto de entrada (`volunteer computing on freechains ipfs and podman works end to end`, contendo **11 palavras**).
   - Inicia `vcnode run` para `bob` e `carol` em background como workers e, em seguida, inicia `alice` com `--submit=<arquivo>`.
   - Os 3 nós executam o comando `sh -c "wc -w < /input/data.txt > /output/result.txt"` dentro do container Alpine isolado pelo Podman, sincronizam commits e reveals via Freechains e trocam os artefatos via IPFS.
6. **Auditoria, Votação e Veredito**:
   - Quando `alice` finaliza e encerra o processo, o script imprime a linha do tempo completa da cadeia (`list order` com tipo de ação e alias do autor), o relatório do Submitter (`JOB_ID`, `ACCEPTED_HASH`, `VERIFIED=true`, `RESULT=11`, `WINNERS`, `DIVERGENT`, `NO_REVEAL`) e a reputação antes e depois de cada nó.
   - Por fim, valida automaticamente as invariantes do modo executado (`honest` ou `--divergent`) e imprime `PASS` (exit 0) ou `FAIL` (exit 1).

---

## Uso Direto da CLI (`vcnode`)

```bash
vcnode run \
  --id=<alias> \
  --root=<diretório raiz do freechains> \
  --key=<caminho da chave privada ssh ed25519> \
  --chain=#vc \
  --peers=localhost:8332,localhost:8333 \
  --behavior=honest|divergent \
  [--submit=<arquivo de entrada>] \
  [--grace=20]
```

**Variáveis de Ambiente:**
- `VC_IMAGE` *(obrigatória)*: referência da imagem fixada por digest (`repo@sha256:...`).
- `VC_IPFS_API` *(opcional)*: URL da API HTTP do Kubo (padrão: `http://127.0.0.1:5001`).
- `VC_PODMAN_SOCKET` *(opcional)*: URI do socket do Podman (padrão: `unix://$XDG_RUNTIME_DIR/podman/podman.sock`).

Na inicialização, o `vcnode` executa `ipfs.Ping`, `sandbox.Ping` e `sandbox.Cleanup` (removendo eventuais containers órfãos com label `vc.managed=true`) antes de iniciar os atores.

---

## Concessões do MVP e Premissas Técnicas

1. **Identidade Autenticada pela Cadeia**: O `worker_id` nunca é lido do payload Protobuf; ele é extraído exclusivamente do campo `sign` dos metadados da ação validada pelo Freechains (`<tipo-chave> <base64>`).
2. **Marcos Baseados em Posição na Cadeia**: As transições de fase (`Committing` to `Revealing` no N-ésimo commit válido to `Done` em `JobFinalize`) dependem unicamente dos índices na ordem consensual da cadeia, nunca de relógio de parede.
3. **Timer Local de Tolerância do Submitter (*MVP Concession*)**: O Submitter finaliza assim que todos os N trabalhadores contabilizados revelam, **ou** quando já existe um grupo com quórum (Q=2) e `--grace` segundos (padrão `20`) se passaram no relógio local do Submitter desde a abertura da fase `Revealing`. Esse timer é estritamente local ao Submitter; nenhum outro nó depende de relógio local e todos transitam para `Done` apenas ao observar `JobFinalize` na cadeia.
4. **Permissões do Diretório de Saída no Podman Rootless**: O container executa como UID/GID `65534:65534` (`nobody`) com filesystem raiz *read-only*. No mapeamento de user namespace do Podman rootless, o UID `65534` corresponde a um *subuid* do host (`165533`), portanto o `WorkerActor` cria o diretório de saída no host com permissão explícita `0777` (e aplica o rótulo SELinux `container_file_t` quando aplicável) antes de iniciar o container.
5. **Materialização de Arquivo Único no IPFS**: Como `ipfs.Store.Add` adiciona um nó de arquivo único sem nome e `ipfs.Store.Fetch` o materializa como `<dest>/content`, o `IPFSAdapter.Fetch` renomeia `<dest>/content` para `<dest>/data.txt` quando busca o arquivo de entrada, permitindo que o container leia `/input/data.txt`.

---

## Implementação

- [x] API Freechains
- [x] Serialização Protobuf
- [x] Sandbox Podman
- [x] IPFS para dados
- [x] Exemplo Simples
- [x] Commit-Reveal
- [x] Quorum
- [ ] Remover opção behavior da CLI
- [ ] API para utilização via código
- [ ] Exemplo via complexo via código

