# 구성 기록 — 2026-09-30

## 설치 및 실행 상태

- 호스트: macOS arm64, OrbStack Docker 29.4.0, Paseo 0.10.1, sbx 0.46.0.
- 여섯 image를 로컬 arm64로 빌드하고 `sbx template load`로 등록했다.
- tag: `2026.09.30`, namespace: `docker.io/paseo-sbx`.
- 각 image에 Codex 0.159.2 또는 Claude Code 2.1.285, Node 24.21.0을 설치했다.
- Superpowers 6.4.2, GSD Core 1.15.0, Ponytail 4.10.0을 각각 설치했다.
- git commit, npm version, base image digest, Node archive checksum은 lock 파일에 고정했다.
- workspace: `~/Repository/playground/codex/sandbox-workspace`.
- sandbox: `paseo-<provider>-86d91e2dcc`, 각각 2 CPU, 2 GiB, `--skills off`.
- OpenAI OAuth secret 등록과 세 Claude sandbox의 Claude 구독 로그인을 완료했다.
- 기존 Paseo에 여섯 provider를 병합했다. 전부 `available` 상태를 확인했다.
- 호스트 runtime은 `~/.local/share/paseo-sbx/86d91e2dcc/`에 설치했다.
- 원래 Paseo 설정 백업: `~/.paseo/config.json.backup-20260930T013132338235Z`.

| Provider | sbx template image ID |
| --- | --- |
| codex-superpowers | eb0ee0a0b9e3 |
| codex-gsd-core | f36c3d3c89a3 |
| codex-ponytail | 52a5e9ed37ad |
| claude-superpowers | 4e87d1ff1a85 |
| claude-gsd-core | fd1a590dc6ea |
| claude-ponytail | 973af7296185 |

## 실제 실행 기록

- 부모 `10891617-a8e1-48c8-afbf-91dfae3f6366`: codex-ponytail/gpt-6-luna.
- sandbox에서 Paseo MCP의 `list_providers`, `list_models`, `create_agent`를 호출했다.
- 자식 `8493a374-965a-419d-8d42-b92a410cc9cb`: codex-superpowers/gpt-6-luna.
- 자식 응답 `PASEO_SBX_CHILD_OK`가 부모에게 전달됐다.
- Paseo inspect의 `ParentAgentId`는 위 부모 ID와 일치했다.
- 별도 claude-gsd-core agent `5e69443b-498f-414c-a152-8fb23515c310`는
  Claude 구독으로 `CLAUDE_SBX_READY`를 반환했다.
- 같은 Codex 부모가 claude-ponytail/claude-haiku-4-5 자식
  `e587a8d2-b5e1-4470-be45-4d55a008ac6c`를 추가 생성했다.
  `CLAUDE_SBX_CHILD_OK` 응답이 부모에게 전달되어 Codex → Claude 위임도 완료됐다.
- 기존 구현 단계의 경계 테스트 10개는 통과했다. 최종 경로 수정 후에는
  자동 테스트를 추가 실행하지 않았고 실제 Paseo provider 조회와 위임으로 연결을 확인했다.

## 실패 원인과 해결

1. 최초 sbx 실행에 network policy가 없어 `sbx policy init balanced`로 초기화했다.
2. 공식 image의 Node/npm 버전이 GSD 요구와 달라 checksum을 고정한 Node 24를 설치했다.
3. Paseo Codex model discovery는 daemon cwd에서 app-server를 시작한다.
   초기 launcher는 그 cwd가 workspace 밖이라는 이유로 조회도 거부했다.
   버전 조회를 별도 처리하고 Codex app-server를 등록 workspace에서 시작하도록 수정했다.
   실제 RPC에 전달되는 cwd는 등록 workspace 안인지 확인한다.
4. Paseo reload가 기존 provider 오류를 캐시하고 있어 daemon restart 후 여섯 provider가
   모두 정상적으로 available 상태가 됐다.
5. 빌드 중 자동 승인 검토가 workspace requirements 로딩 실패로 한 번 실행을 거부했다.
   로컬 지침 위치를 확인하고 재시도한 빌드는 승인되어 완료됐다.

## 재사용 범위와 제한

- GitHub 소스와 GHCR 수동 게시 workflow를 준비했다. 실제 원격 게시를 수행하지 않았다.
- 로컬 arm64 image는 빌드했다. amd64 빌드는 GitHub workflow에 구성했으며 아직 실행하지 않았다.
- image에는 모델 credential이나 로그인 이후 sandbox snapshot을 포함하지 않는다.
- 여섯 sandbox의 파일은 같은 workspace를 공유한다. 동시 편집은 파일별로 나누어 배정한다.
- native plugin hook의 trust와 실제 hook 실행은 별도이다. Codex `/hooks`에서 검토한다.
- GSD 공식 Codex installer의 일부 `.claude/` project 경로 참조 경고는 upstream 사항으로 남겼다.
- Paseo가 호스트에서 native session 파일을 직접 읽는 기능은 sandbox session 경로에 접근할 수 없다.

## 소스 위치 변경

2026-09-30 사용자 요청으로 소스와 운영 스크립트를
`~/devsbx/paseo-sbx`로 이동했다. 생성된 Bake context도 갱신했다.
기존 sandbox의 workspace와 호스트 runtime 경로는 위 구성 기록과 같다.

## Go 전환 2026년 9월 30일

현재 실행 도구는 Go로 구현했다. 위의 설치 및 실행 결과는 최초 구성 당시의 기록이다.

CLI, launcher, MCP relay, checksum으로 고정된 Node 설치 도구를 Go 표준 라이브러리로
전환했다. 기존 진입점, 소스와 테스트를 교체하고 `cmd/`, `internal/`, `go.mod`,
Makefile을 추가했다. `make build`가 세 바이너리를 생성하며 `./paseo-sbx`는 Go CLI를
실행한다. 기존 registry 구조, workspace hash, 여섯 profile과 명령 옵션을 유지한다.
Paseo에 이미 설치된 runtime은 `register --workspace <path> --apply`로 갱신한다.
이번 변경은 기존 사용자 Paseo 설정과 실행 중인 sandbox에 자동 적용하지 않았다.

Go 테스트로 workspace 경계, JSON 및 SSE 필터링, HTTP capability 인증, 자식 agent
접근 범위, credential 제거, CLI lifecycle, 설정 백업 및 병합, checksum과 다운로드
실패를 확인했다. 독립 코드 검토에서 발견한 symlink 뒤의 부모 경로 처리, 형식이
잘못된 upstream 응답, 실패한 create의 registry 보존 문제를 회귀 테스트로 수정했다.

검증 결과:

- `go test ./...`, `go test -race ./...`, `go vet ./...` 통과.
- `make build` 및 macOS/Linux의 arm64/amd64용 세 바이너리 교차 빌드 통과.
- 소스 트리 밖에서 CLI help 및 Bake JSON 생성 확인.
- Docker의 Go installer 빌드 단계와 Codex 기반 linux/arm64 Node runtime 단계 빌드 통과.
- 실제 Node archive 다운로드, lock 파일의 SHA256 검증과 설치 통과.

실제 컨테이너 검증에서 base image에 xz 실행 파일이 없음을 확인했다. Dockerfile에
xz-utils 설치를 추가하여 Go 설치 도구의 tar.xz 해제를 보장했다. 여섯 최종 에이전트
이미지 전체 재빌드, sbx 재등록, Paseo와 실제 모델 호출은 이번 검증에 포함하지 않았다.

## Go runtime 등록과 잔여 파일 정리

후속 요청으로 문서, 코드 주석, ignore 규칙과 생성 설정의 이전 구현 참조를 정리했다.
`register --apply`로 기존 `86d91e2dcc` runtime의 launcher를 Go 바이너리로 교체하고,
여섯 provider 명령을 직접 바이너리 실행으로 갱신했다. 기존 Paseo 설정을 백업하고
reload를 완료했다. 설치된 runtime의 이전 모듈과 캐시도 제거했다.
복구용 설정 백업은 유지한다.

## 코드 리뷰 수정과 작업 위치 기반 연결 2026년 9월 30일

코드 리뷰에서 확인한 문제를 수정했다.

- paseo-sbx 소스 폴더(또는 그 상위 폴더)를 workspace로 지정하면 `create`와 launcher 설치를 거부한다.
  sandbox가 호스트에서 실행되는 launcher 원본을 바꾸는 경로를 막는다.
- CI workflow의 `workflow_dispatch` 비교 문자열에 따옴표를 추가했다.
- relay는 upstream 도구 오류(`isError`)를 정책 거부 대신 일반 도구 오류로 전달한다.
  SSE 응답의 서버 알림과 서버 요청은 해당 이벤트만 제외한다.

[작업 위치 기반 연결 설계](superpowers/specs/2026-09-30-workspace-routing-design.md)를 구현했다.

- Paseo 0.10.1 앱 번들 코드에서 provider 프로세스가 세션마다 실행되고, Codex app-server는
  daemon cwd에서 실행되며, 세션 환경에 `PASEO_AGENT_CWD`가 추가됨을 확인했다.
- 공용 launcher `~/.local/share/paseo-sbx/launcher/agent-wrapper`가 `PASEO_AGENT_CWD`
  또는 프로세스 cwd를 포함하는 workspace의 sandbox로 연결한다. 세션 경로가 없는 Codex
  app-server와 `--version`은 해당 조합이 활성인 workspace 중 경로 순 첫 번째를 사용한다.
- `register --apply`는 모든 workspace의 활성 조합으로 provider를 다시 만들고
  workspace별 이전 launcher를 삭제한다. `unregister`와 `list --all`을 추가했다.
- `~/.local/share/paseo-sbx`를 포함하거나 그 안에 있는 workspace를 거부한다.
- GUI 사용 절차를 [README_GUI.md](../README_GUI.md)에 정리했다.

검증: `go vet ./...`, `go test -race ./...` 통과. 가짜 `sbx`·`paseo`로 프로젝트 5개(A·B·C
superpowers, D gsd-core, E ponytail)를 생성·등록해 provider 6개가 공용 launcher를 가리키고,
A·B 동시 실행이 각자의 sandbox로 연결되며, 다른 프로젝트 조합 선택과 unregister 후 실행이
거부됨을 확인했다.

실제 환경 이전: 사용자 승인 후 `register --workspace .../codex/sandbox-workspace --apply`를
실행했다. 기존 workspace 4개(`59b27d3be6`, `86d91e2dcc`, `eaaa22a0e0`, `f51b18107f`)의
활성 조합으로 provider 6개를 다시 만들었고 모두 공용 launcher를 가리킨다. 다른 Paseo 설정은
유지했다. 설정 백업은 `~/.paseo/config.json.backup-20260930T070346.686631000Z`이며
Paseo reload를 완료했다. 사용자가 실행한 `paseo provider ls`에서 SBX provider 6개가 모두
`available` 상태임을 확인했다. GUI 세션 확인은 아직 수행하지 않았다.

삭제된 workspace 처리: 폴더를 삭제한 workspace의 registry가 `list --all`에 계속 표시되고,
세션 경로가 없는 실행의 대체 sandbox로 선택될 수 있었다. 삭제된 workspace는 연결과
provider 목록에서 제외하고 `list --all`에 `missing`으로 표시한다. `prune`(`--dry-run` 지원)은
registry를 삭제하고 provider를 다시 등록한다. 사용자 결정에 따라 sandbox는 삭제하지 않고
`sbx rm` 명령만 안내한다. 실제 환경 dry run에서 삭제된 workspace 3개(methology의
superpowers, ponytail, gsd-core)와 sandbox 10개를 확인했다.

정리 작업(사용자 승인 범위): `prune`으로 삭제된 workspace 3개의 registry를 제거하고 Paseo를
reload했다(설정 백업 `~/.paseo/config.json.backup-20260930T080322.744393000Z`). 해당 sandbox
10개를 `sbx rm`으로 삭제했다. 어느 registry도 사용하지 않는 superpowers template·이미지 2개
(`-7388c6b052ad`, `-836bc7684b34`)와 Go 전환 검증용 이미지(`go-node-runtime-check:local`,
`go-installer-check:local`)를 삭제했다. 정리 후 workspace 4개, sandbox 12개, template 12개가
모두 registry와 일치하고 SBX provider 6개는 `available` 상태다.

## 2026-10-01 Antigravity CLI 초기 구현 (폐기)

설계: catalog에 `antigravity-cli`를 추가하고 `native`와 `superpowers` 조합을 제공한다.
sbx에는 해당 내장 agent가 없으므로 `shell` sandbox를 만들며, Paseo에는 Google의
공식 별도 ACP 서버를 `extends: "acp"`로 연결한다. launcher가 workspace와 MCP relay를
기존처럼 검증하고, ACP의 호스트 파일·터미널 기능은 끈다. 인증은 각 sandbox에서
수행하고 호스트 인증 폴더를 공유하지 않는다.

구현: `agy` 1.2.14와 ACP 1.2.1의 공식 artifact URL·checksum을 lock에 고정했다.
Superpowers 스킬은 CLI와 ACP의 두 global 경로에 설치한다. `antigravity-login`은
Google OAuth callback URL을 숨겨 입력받아 sandbox 내부의 공식 loopback 서버로
전달한다. `--api-key`는 Gemini API key 모드를 선택하며 실제 키는 sbx proxy가 관리한다.
GSD Core·Ponytail·RTK·Headroom 조합은 지원하지 않는다.

실제 sbx에서 공식 arm64 ACP가 16KB 페이지 때문에 초기화 시 abort하는 문제를 확인했다.
arm64 이미지에서는 공식 amd64 ACP 서버와 `localharness_external`을 QEMU로 실행하고,
필요한 amd64 런타임을 별도 pinned image에서 복사한다. `agy`는 arm64 네이티브다.
ACP에는 에뮬레이션 성능 비용이 있으며 초기화 중 upstream hugepage 정렬 경고가
stderr에 출력되지만 세션 생성에는 성공했다.

검증: `make build`, `make test`(Go race), `make vet` 통과.
최종 native·Superpowers arm64 이미지를 빌드하고 CLI·스킬 설치를 확인했다.
최종 native 이미지를 sbx에 로드하여 인증 helper의 API 모드 선택과 공용 launcher →
sbx → 공식 ACP의 initialize·session/new를 검증했다. Paseo의 NO_BROWSER 모델 탐색
경로에서 workspace를 올바르게 선택하고 모델 14개를 반환했다. 실제 Google 계정
로그인·모델 요청·Paseo GUI 대화·amd64 이미지 실행은 수행하지 않았다.
사용자 workspace registry 및 Paseo provider 설정은 변경하지 않았다.

인증 helper 언어 수정: `cmd/antigravity-login`과 `internal/antigravitylogin`으로
helper와 테스트를 Go로 통일했다. 이번 변경에서 추가했던 Python 파일·설치 의존성·
Makefile 및 CI 실행 단계를 제거했다. Docker의 기존 Go builder에서 대상 아키텍처용
정적 바이너리를 빌드한다. callback은 로그인 시도별 state·loopback·port·path를 검증하고
HTTP proxy와 redirect를 사용하지 않는다. 기존 CLI 설정을 보존하며 인증 모드를
선택하고, 취소 시 ACP 프로세스를 종료한다. 실제 계정 로그인을 대체하는 가짜 ACP와
loopback endpoint로 OAuth·API 모드, 설정 권한, 취소 동작을 Go race 테스트로 검증한다.

수정 검증: 전체 Go race 테스트와 vet 통과, helper의 darwin/linux × arm64/amd64
교차 빌드 성공. native·Superpowers 이미지를 Go helper로 재빌드했다. 최종 native
이미지로 만든 실제 sbx에서 `antigravity-login --api-key`가 공식 ACP 서버와 통신하여
인증 모드를 선택하는 데 성공했다. 실제 계정 로그인·모델 요청은 수행하지 않았다.

## 2026-10-01 기존 Antigravity 플러그인으로 전환

초기 ACP 설계는 설치된 `paseo-plugin-antigravity-cli` 0.8.0을 확인하기 전에 전용
연동이 없다고 가정한 오류였다. 해당 구현의 통과 테스트는 그 전제를 검증하지 못했다.
ACP artifact·인증 helper·QEMU·호환 image·전용 설치 script를 제거했다.

새 구현은 기존 Go 설치기를 `install-runtime`으로 일반화하여 공식 `agy`의 SHA-512를
검증한다. catalog·공용 Dockerfile·공용 launcher에 Antigravity를 통합하고,
`paseo-sbx run`이 local daemon WebSocket API로 기존 native 공급자에 에이전트를 생성한다.
설치된 플러그인이 stream-json을 처리하며 `agyPath`와 `extraArgs`로 SBX 프로파일을
선택한다. 별도 TypeScript entry와 SBX 공급자 alias는 추가하지 않는다.
Codex·Claude 공급자 등록은 유지한다. 기존에 등록한 자체 ACP 항목은 다음 register 때
해당 trusted launcher의 항목만 정리한다. 사용자 Paseo config는 자동 수정하지 않았다.

첨부·schema·명령용 스킬은 plugin-data에서 guest 임시 경로로 복사하고 종료 시 정리한다.
외부 일반 경로·symlink는 거부하며 호스트 credential과 HOME은 guest에 전달하지 않는다.
guest 기록에 의존하는 질문·background·subagent 추적은 지원하지 않는다.
모델 목록·호스트 전역 설정은 기존 플러그인의 호스트 기준이다.

검증: 전체 Go race 테스트·vet, macOS/Linux × arm64/amd64 Go 교차 빌드 통과.
native·Superpowers image를 빌드하고 CLI·스킬 설치와 ACP/helper 제거를 확인했다.
native image의 sbx 로드·공식 CLI 실행도 확인했다.
실제 Paseo 0.10.2에서 검증용 에이전트를 생성하고, network 코드가 없는 시험용 Go agy의
checksum을 확인한 뒤 플러그인 → 공용 launcher → sbx의 native 응답을 확인했다.
cwd는 등록한 workspace, HOME은 `/home/agent`였고 plugin 파일은 guest 임시 경로로
전달됐다. 실제 Google 로그인과 모델 요청은 수행하지 않았다.
