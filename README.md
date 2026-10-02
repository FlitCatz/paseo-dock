# Paseo + Docker Sandboxes

Go 관리 TUI에서 workspace와 sandbox 상태를 보고 생성·업데이트·shell·Paseo 연결을 관리합니다.
각 에이전트의 파일·shell·하위 프로세스는 독립된 Docker Sandbox 안에서 실행됩니다.

## 관리 TUI

`make build` 후 터미널에서 `./bin/paseo-sbx`를 인수 없이 실행하거나
`./bin/paseo-sbx tui --workspace /absolute/path/to/project`로 초기 workspace를 선택합니다.
인수 없는 실행의 stdin/stdout이 터미널이 아니면 usage와 종료 코드 2를 반환합니다.
`./bin/paseo-sbx --help`는 비대화형 환경에서도 종료 코드 0입니다.
기존 CLI 명령과 `create`의 독립 생성 wizard도 계속 사용할 수 있습니다.

| 키 | 동작 |
| --- | --- |
| Tab / Shift+Tab | workspace, sandbox, 상세, 로그 패널 이동 |
| ↑↓ / j k | 목록 선택 또는 상세·로그 스크롤 |
| ←→ / h l | 상세·로그 탭 전환 |
| /, Enter, Esc | 검색 시작, 입력 확정, 검색 해제·화면 취소 |
| r / ? | 상태·연결·template 수동 갱신 / 도움말 |
| Enter | 선택 대상의 작업 메뉴; ↑↓/jk 선택, Enter 확인·실행, Esc 취소 |
| u / s | 선택 sandbox 업데이트 확인 / shell·로그인 진입 |
| n | 경로·메모리 입력과 directory picker → 선택 wizard → 생성 확인 |
| a | 저장된 선택으로 build/load, template 보기, 누락 workspace 정리 미리보기 |
| t / v | template 탭 / 미등록 sandbox의 읽기 전용 보기 |
| c / R | 실행 작업 취소 요청·대기 작업 취소 / 최근 실패 작업의 고정 대상 재시도 확인 |
| q / Ctrl+C | 종료; 작업 중이면 계속 실행 또는 취소 후 종료 선택 |

120×40 이상에서는 여러 패널을 함께 보여 주고, 80×24에서는 상세·로그 탭을 사용하며,
작은 화면에서는 포커스 패널을 표시합니다. 한글 표시 너비에 맞춰 내용을 줄이고,
`NO_COLOR=1`을 설정하면 색상 스타일을 생략합니다. shell 종료 후 관리 화면으로 돌아옵니다.
shell은 실행 중인 변경 작업과 대기열이 모두 끝난 뒤 사용할 수 있습니다.

registry와 `sbx ls --json`은 3초 간격, Paseo·Docker 연결은 15초 간격으로 조회합니다.
template은 탭 진입과 수동 갱신 때 읽습니다. 자동 조회는 guest를 실행하지 않습니다.
연결 실패 시 마지막 목록과 확인 시각을 보존하고 현재 runtime을 확인 불가로 표시합니다.
등록하지 않은 sandbox는 읽기 전용이며, registry 활성 상태와 실제 Paseo 등록을 구분합니다.
Antigravity native plugin provider는 관리 launcher provider가 아니므로 `unmanaged`로 표시됩니다.

상세의 **이미지 기준 (guest 미확인)**은 lock의 버전이고, **마지막 확인**은 현재 sandbox·image와
일치하는 guest 확인 기록입니다. 작업 메뉴의 **guest 버전 확인**은 바이너리·설치 기록만 읽습니다.
**업데이트**는 installer를 실행하며 선택 sandbox 또는 workspace의 활성 조합 전체에 적용할 수
있습니다. 버전 확인·업데이트·shell은 `sbx exec`를 사용하므로 중지된 guest를 시작할 수 있습니다.
관리 화면의 shell 진입은 숨은 자동 업데이트를 수행하지 않습니다. 기존 CLI `shell`과 launcher는
기존 자동 갱신 정책을 유지합니다. 중지는 실행 중인 세션에 영향을 줄 수 있어 대상 확인이 필요합니다.

변경 작업은 한 번에 하나씩 실행하며 조회·탐색은 계속할 수 있습니다. 대상·선택·옵션은 확인 때
고정되고 실행 직전에 다시 검증합니다. 최대 20개 작업을 보관하고 작업별 로그는 256 KiB의 최근
출력으로 제한합니다. `c`는 호스트 subprocess와 자식 process group에 취소를 전달하지만 guest
installer 중단을 증명하지 못하면 **확인 필요**로 표시합니다. sandbox stop이나 완료된 변경의
자동 rollback은 하지 않습니다. 작업 중 `q`/Ctrl+C는 `k`/Esc로 계속 실행하거나 `c`/Enter로
취소 후 완료까지 기다리고 종료할 수 있습니다.

생성 확인에는 실제 조합·utilities·CPU 2·메모리·build/load·Paseo 적용 여부가 표시됩니다.
이미지·설정이 다른 기존 sandbox는 자동 재사용하지 않습니다. 일부 생성 또는 OpenSpec 초기화가
실패하면 실제 남은 대상과 오류가 로그에 남습니다. 로그와 guest 상태를 확인한 뒤 `R`로 같은
대상·선택을 재시도하거나 수정한 선택으로 새 생성 흐름을 시작합니다. Paseo 해제는 sandbox와
완료된 saved selection을 보존하며, 관리 화면의 등록은 저장된 선택과 profile entry를 검증하여
명시적으로 활성 상태를 복원할 수 있습니다. 기존 CLI `register`의 검증은 그대로 유지됩니다.
고급 정리는 미리보기의 누락 workspace만 다시 검증하여 registry·selection·launch routing을
정리하며 sandbox와 update 기록·lock을 보존합니다.

검증 기록은 [관리 TUI 설계 상태](docs/2026-10-02-management-tui-plan.md)에 있습니다.
실제 PTY의 shell 복귀·터미널 복원과 fake 외부 명령 테스트는 통과했습니다.
폐기용 실환경에서 생성·중지·shell·격리된 Paseo 등록/해제는 확인했고, Antigravity 업데이트는
공식 manifest HTTP 403으로 부분 실패했습니다. 모든 단계가 성공하는 live lifecycle은 아직 미검증입니다.

기본 카탈로그에서 선택 가능한 조합은 다음과 같습니다. 모두 자동 생성되는 것은 아닙니다.

| Paseo provider | 에이전트 | Methodology |
| --- | --- | --- |
| codex-superpowers | Codex | Superpowers |
| codex-gsd-core | Codex | GSD Core |
| codex-ponytail | Codex | Ponytail |
| codex-openspec | Codex | OpenSpec |
| claude-superpowers | Claude Code | Superpowers |
| claude-gsd-core | Claude Code | GSD Core |
| claude-ponytail | Claude Code | Ponytail |
| claude-openspec | Claude Code | OpenSpec |
| antigravity-cli-native | Antigravity CLI / Paseo native plugin | Native (methodology 없음) |
| antigravity-cli-superpowers | Antigravity CLI / Paseo native plugin | Superpowers skills |
| antigravity-cli-gsd-core | Antigravity CLI / Paseo native plugin | GSD Core |
| antigravity-cli-ponytail | Antigravity CLI / Paseo native plugin | Ponytail plugin |
| antigravity-cli-openspec | Antigravity CLI / Paseo native plugin | OpenSpec |

## 새 머신에 설치

소스 빌드에는 Go 1.26+가 필요하고, 실행에는 Docker/Buildx, Paseo 0.10.1+, sbx 0.46.0+가 필요합니다.
Go 바이너리는 별도 언어 런타임 없이 실행됩니다.
macOS에서는 Apple silicon과 macOS 14+가 필요합니다. sbx는 Docker Desktop과
별도 설치 도구이며, 이미지 빌드에는 Docker 데몬이 필요합니다. 이 프로젝트는
OrbStack에서 이미지를 빌드하고 sbx의 독립 runtime으로 실행할 수 있습니다.

```bash
brew install go docker/tap/sbx
make build
sbx login
sbx policy init balanced   # 새 sbx 설치에서 한 번만 실행

./paseo-sbx create --workspace /absolute/path/to/project
```

이미 소스와 바이너리가 있는 머신에서는 `make build`로 갱신한 뒤 마지막 명령을 실행합니다.
TUI의 최종 확인 화면에서 **Paseo에 선택한 provider 등록 및 reload**를 선택하면
생성과 등록을 한 번에 진행합니다. 선택하지 않았다면 생성 후 다음 명령으로 등록합니다.

```bash
./paseo-sbx register --workspace /absolute/path/to/project --apply
```

터미널에서 선택 옵션 없이 `create`를 실행하면 Charmbracelet Bubble Tea TUI가 열립니다.
Agent·methodology·utility 목록을 한 화면에 표시하고, 현재 선택 그룹을 강조합니다.
Agent를 복수 선택하고, 각 agent에 사용할 methodology를 복수 선택한 뒤, 유틸리티를
선택합니다. 방향키로 이동, Space 또는 x로 선택, Enter 또는 Tab으로 다음 그룹,
Esc 또는 Shift+Tab으로 이전 그룹, Ctrl+C로 취소합니다. 체크 상태는 이전 그룹으로
돌아가도 유지됩니다. 작은 터미널에서는 현재 항목을 따라 스크롤하고, `NO_COLOR=1`로
색상을 끌 수 있습니다. 마지막 화면에 실제 생성할 조합과 Paseo 등록 여부가 표시되며,
이 화면에서는 Enter로만 생성을 확정합니다.
확정하면 선택한 이미지 빌드 → sbx template 로드 → sandbox 생성 순서로 실행합니다.
이미 로드된 template을 사용하려면 `--skip-build`를 전달합니다.
선택 옵션을 지정한 상태에서도 TUI를 열려면 `--interactive`를 전달합니다.
`--workspace`를 생략하고 TUI를 실행하면 현재 디렉터리를 workspace로 사용합니다.

예를 들어 Codex에 Superpowers·Ponytail을 선택하면 sandbox 두 개만 생성합니다.
모델 작업은 시작하지 않습니다. 기본 자원은 sandbox당 2 CPU와 2 GiB입니다.
필요하면 `--memory 4g`로 늘립니다.

OpenSpec은 세 에이전트 모두 지원합니다. 선택한 이미지에 `@fission-ai/openspec` 1.14.0을
설치하고, sandbox 생성 후 공식 CLI로 해당 workspace의 프로젝트 파일을 초기화합니다.
`openspec/`과 선택한 에이전트용 skills·commands/workflows를 생성하며 기존 프로젝트
설정을 재사용합니다. 여러 OpenSpec 에이전트를 선택하면 한 번에 통합 초기화합니다.
호스트에 Node/OpenSpec을 설치하지 않으며 telemetry는 끕니다. 초기화 실패 시 기존
활성 선택을 유지하고, 생성한 sandbox 기록을 남겨 재시도할 수 있습니다.

```bash
./paseo-sbx create --workspace /path/to/project \
  --profiles codex-openspec,claude-openspec,antigravity-cli-openspec --build
```

기본 workflow는 공식 `core` profile입니다. 사용법은
[OpenSpec 공식 문서](https://github.com/Fission-AI/OpenSpec/blob/main/docs/cli.md)를 참고하세요.

비대화형 실행에는 명시적인 선택 또는 해당 workspace에 저장된 선택이 필요합니다.
선택 없이 전체 조합을 자동 생성하지 않습니다.

```bash
./paseo-sbx create --workspace /absolute/path/to/project \
  --profiles codex-superpowers,codex-ponytail --utilities rtk,headroom --build --apply

# 같은 methodology를 선택한 모든 agent에 적용
./paseo-sbx create --workspace /another/project \
  --agents codex,claude --methodologies superpowers --utilities rtk --build

# 분리해서 실행할 때는 같은 선택을 전달
./paseo-sbx build --profiles codex-superpowers --utilities rtk
./paseo-sbx load --profiles codex-superpowers --utilities rtk
./paseo-sbx create --workspace /project --profiles codex-superpowers --utilities rtk
```

활성 선택은 신뢰한 호스트 runtime의 `registry.json`에 원자적으로 저장하고,
가져오기용 사본을 `selection.json`에 기록합니다. 다음 실행에서
`build`와 `load`에 `--workspace /project`를 지정하면 저장된 선택을 재사용합니다.
터미널에서 선택 옵션 없이 `create --workspace /project`를 실행하면 저장된 선택을
미리 체크한 TUI가 열립니다. 비대화형 `create`에서는 저장된 선택을 그대로 사용합니다.
`--selection /path/to/selection.json`으로
선택 파일을 가져올 수도 있습니다. `--utilities ''`는 유틸리티를 명시적으로 제외합니다.
`--profile`, `--profiles`, `--agents/--methodologies`로 새 조합을 직접 지정할 때는
utilities도 함께 지정하세요. 이 경우 `--utilities`를 생략하면 유틸리티 없이 구성합니다.
예시 선택 파일은 다음과 같습니다.

```json
{"schema":1,"pairs":[{"agent":"codex","methodology":"superpowers"}],"utilities":["rtk"]}
```

`build`는 선택한 Bake target만 생성합니다. CLI, plugin 소스, GSD·Node·utility 버전,
다운로드 checksum과 base image digest를 고정합니다. 이미지 태그에는 설치 구성 hash를
포함합니다. `load`는 Docker의 image store에서 tar를 내보내 sbx에 등록하고 임시 tar를 삭제합니다.

`register`는 적용할 설정을 출력합니다. `--apply`는 기존 `~/.paseo/config.json`을
백업하고 등록된 모든 workspace의 활성 조합으로 provider와 MCP injection 설정을 병합한 뒤
Paseo를 시작하거나 reload합니다. 어느 workspace에서도 활성이 아닌 조합의 provider는 제거합니다.
paseo-sbx가 등록하지 않은 provider는 보존합니다.
다른 Paseo home에는 `--paseo-home /path/to/home`을 전달합니다.
launcher 오류를 수정한 뒤에도 모델 조회 오류가 남으면 `paseo daemon restart`로
Paseo의 기존 provider 오류 캐시를 초기화합니다.

## Go 빌드와 기존 설치 갱신

`make build`는 `bin/paseo-sbx`, `bin/agent-wrapper`, `bin/install-runtime`을 만듭니다.
저장소의 `./paseo-sbx`는 빌드된 CLI를 실행하는 shell 진입점입니다. 다른 디렉터리에서도
절대 경로로 실행할 수 있습니다. 바이너리를 소스 트리 밖으로 옮긴 경우에는
`PASEO_SBX_ROOT=/path/to/paseo-sbx`로 Dockerfile과 lock 파일의 위치를 지정합니다.
CLI와 함께 `agent-wrapper` 바이너리도 같은 디렉터리에 두세요.

기존 launcher로 등록한 workspace는 다시 빌드한 뒤 다음 명령으로 갱신합니다.
기존 sandbox와 로그인 상태를 재사용하고, Paseo 설정을 백업한 뒤 실행 명령을 변경합니다.
`register`는 지정한 workspace만이 아니라 등록된 모든 workspace의 활성 조합으로 provider를
다시 만들고, 공용 launcher로 옮긴 뒤 workspace별 이전 launcher를 삭제합니다.
여러 workspace를 사용 중이어도 한 번만 실행하면 됩니다.

```bash
make build
./paseo-sbx register --workspace /absolute/path/to/project --apply
```

Go Node 설치 도구를 포함한 새 이미지가 필요하면 `build`와 `load`를 실행합니다.
이미 실행 중인 sandbox의 이미지는 자동 교체하지 않습니다.

## 기존 sandbox 내부 자동 업데이트

Paseo launcher가 agent 세션을 시작하거나 `paseo-sbx shell`로 들어갈 때,
sandbox별로 하루 한 번 agent·설치된 methodology·선택한 utility를 업데이트합니다.
이미지를 다시 만들거나 sandbox를 삭제하지 않으므로 기존 로그인과 세션을 유지합니다.
버전 조회(`--version`)는 업데이트를 실행하지 않습니다. 변경된 launcher를 기존 설치에
적용하려면 `make build` 후 위의 `register --workspace /project --apply`를 실행합니다.

즉시 갱신하려면 다음 명령을 사용합니다. profile을 생략하면 해당 workspace의 모든
활성 조합을 갱신합니다. 일부 조합이 실패해도 나머지는 처리하고 실패 종료 코드를 반환합니다.

```bash
./paseo-sbx update --workspace /path/to/project
./paseo-sbx update --workspace /path/to/project --profile codex-superpowers
```

Codex·Claude·OpenSpec은 npm의 `latest` 버전을 조회합니다. 새 버전은 사용자 소유의
별도 디렉터리에 설치하고 `--version` 검증 후 `~/.local/bin`의 실행 파일을 원자적으로
전환합니다. 이미 최신 버전이면 다시 설치하지 않습니다. GSD Core는 최신 npm 버전의
공식 전역 설치기를 실행합니다. Superpowers·Ponytail은 설치된 Git repository의 기본
branch HEAD로 갱신하고 agent별 plugin 등록을 갱신합니다. checkout에 로컬 변경이 있으면
덮어쓰지 않고 해당 methodology의 업데이트 실패를 기록합니다.

Antigravity는 [공식 설치기](https://antigravity.google/docs/cli/install/)가 사용하는 최신
release manifest에서 바이너리를 다운로드하여 SHA512 검증 후 전환합니다. shell profile은
변경하지 않습니다. RTK는 최신 GitHub release의 아키텍처별 바이너리를 SHA256 검증 후
전환합니다. Headroom은 하위 의존성까지 새 버전을 확인하고 변경이 있으면 기존 Python으로
새 가상환경에 최신 패키지를 설치하고 검증 후 전환합니다.
Node·Python·uv·OS 패키지·base image는 이 업데이트 대상이 아니며 이미지의 고정 구성을
사용합니다. OpenSpec의 프로젝트 파일은 자동 재생성하지 않습니다.

같은 sandbox의 중복 업데이트는 호스트 파일 lock으로 막습니다. 업데이트는 sandbox당
최대 3분이며, 자동 업데이트 실패 시 설치된 agent로 실행을 계속하고 한 시간 뒤 재시도합니다.
npm agent·Antigravity·OpenSpec·RTK·Headroom의 설치·검증 실패는 기존 실행 파일을 보존합니다.
GSD·plugin 설치기는 자체 설정을 변경하므로 전체 구성의 transaction을 보장하지
않습니다. 이미 실행 중인 agent는 다음 세션부터 새 버전을 사용합니다.

업데이트 확인 시각·실제 버전·실패 정보는 호스트 runtime의 `updates/*.json`에 저장합니다.
guest의 기존 `installed.json`과 `versions.lock.json`은 이미지 최초 설치 기록입니다.
업데이트 로그는 stderr로만 출력하며 Paseo 통신 stdout에 섞지 않습니다.
이전의 별도 설치 디렉터리는 실행 중 세션을 위해 보존합니다.
호스트 launcher 환경에 `PASEO_SBX_AUTO_UPDATE=0`을 설정하면 자동 갱신을 끕니다.
수동 `update`는 이 설정과 확인 간격에 관계없이 실행합니다.

선택 기능 도입 전에 생성한 workspace에는 저장된 선택이 없습니다. 최초 빌드·생성에는
TUI 또는 `--profiles` 등으로 조합을 지정해야 합니다. 기존 날짜만 포함한 이미지 태그와
새 구성 hash 태그가 다르면 `create`가 재사용을 거부합니다. 새 workspace에 필요한 조합을
생성하는 것이 가장 간단하며, 기존 workspace를 계속 쓰려면 보존할 작업과 세션을 확인한 뒤
해당 sandbox를 명시적으로 정리하고 다시 생성합니다. `register --apply`만으로 기존 이미지에
RTK·Headroom이 추가되지는 않습니다.

Docker 실행 없이 생성할 Bake 설정을 확인하려면 다음 명령을 사용합니다.

```bash
./paseo-sbx build --profiles codex-superpowers --utilities rtk \
  --platforms linux/amd64,linux/arm64 --print-bake
```

소스는 `cmd/`의 세 진입점과 `internal/`의 CLI, TUI, catalog·registry, launcher, relay, Node 설치
패키지로 구성됩니다. `make test`는 race 검사를 포함하고 `make vet`는 정적 검사를 수행합니다.

## 유틸리티와 확장

RTK는 고정 버전 바이너리를 설치하고 Codex·Claude의 공식 agent integration을 설정합니다.
Antigravity에서도 모든 방법론과 함께 선택할 수 있습니다. RTK 0.50.0의 공식
Antigravity 지침을 생성하여 sandbox 내부 `~/.gemini/GEMINI.md`에 추가합니다.
이 연동은 에이전트가 `rtk` 명령을 사용하도록 안내하는 방식입니다. 기존 sandbox 지침은
보존하고 호스트 설정과 프로젝트 파일은 변경하지 않습니다.
Headroom은 고정 버전 uv로 sandbox 내부에 전용 Python 런타임과 가상환경을 설치합니다.
호스트 CLI·launcher는 Go이며 호스트에 Headroom용 Python을 설치하지 않습니다.
Headroom을 선택하지 않은 이미지에는 이 전용 런타임을 추가하지 않습니다.

Headroom은 `headroom mcp serve`를 agent에 등록해 압축·조회 도구를 제공합니다.
모든 모델 요청을 자동으로 압축하는 proxy 설정은 포함하지 않습니다.
Paseo의 delegation MCP와 별도로 동작합니다. 유틸리티의 설치 정보는 sandbox의
`~/.local/share/paseo-sbx/utilities/installed.json`에 기록합니다.

기본 카탈로그는 `internal/config/catalog.json`입니다. 소스 root의 `catalog.json`을
추가하면 기본 카탈로그를 대체합니다. `schema`, `agents`, `methodologies`, `utilities`
구조를 유지하고 ID는 소문자·숫자·하이픈으로 정의합니다. 모든 항목은 `id`, `label`을
가지며 methodology·utility에는 지원 agent ID 배열 `agents`가 필요합니다.

- Agent: `adapter`는 현재 `codex`, `claude`, `antigravity-cli`. lock의 `agents`, `base_images`에 동일 ID의 버전과 digest를 추가합니다. Antigravity는 `agent_artifacts`에 공식 CLI 다운로드 URL·SHA-512 checksum도 필요합니다.
- Methodology: `installer`는 `git-plugin` 또는 `gsd-core`. lock의 `tools`에 repository/revision 또는 package/version을 추가합니다.
- Utility: 현재 installer는 `rtk`, `headroom`. 별칭 ID를 쓰면 lock 항목에도 `installer`를 지정하고 같은 installer의 다운로드·런타임 필드를 정의합니다.

새로운 실행 프로토콜이나 설치 방식에는 Go adapter 또는 installer 구현이 필요합니다.
지원하지 않는 항목은 기존 agent처럼 실행하지 않고 오류로 안내합니다.

기존 sandbox의 설치 구성과 새 선택이 다르면 재사용을 거부하고 차이를 안내합니다.
실행 중 sandbox를 자동 삭제·교체하지 않습니다. 다른 workspace에서 새 구성을 생성하거나,
기존 sandbox를 명시적으로 정리한 뒤 다시 생성합니다. 선택만 줄이면 제외된 sandbox는 남고 활성 매핑만 줄어듭니다.

## 모델 인증


Docker 로그인과 모델 서비스 로그인은 별개입니다. sbx secret proxy에 credential을
등록하면 에이전트 이미지에 credential을 넣지 않고 모델을 사용할 수 있습니다.

```bash
# ChatGPT 로그인
sbx secret set openai --oauth

# OpenAI API key 또는 Anthropic API key: 대화형 입력
sbx secret set openai
sbx secret set anthropic
```

Claude 구독 로그인은 선택해서 생성한 각 Claude sandbox에서 Claude Code의 `/login`으로 진행합니다.
예를 들어 `claude-superpowers`를 생성했다면 `./paseo-sbx shell claude-superpowers`로
들어가 `claude`를 실행합니다. 다른 Claude 조합도 생성했다면 그 sandbox에서도 로그인합니다.
API key를 사용하면 `sbx secret set anthropic`으로 생성한 Claude sandbox들에 공통 적용할 수 있습니다.
대화형 Claude 화면 대신 `sbx exec -it <sandbox-name> claude auth login --claudeai`로도
구독 인증을 진행할 수 있습니다. 새 sandbox에서는 다시 인증합니다.
최초 배포용 template에 인증 후의 sandbox snapshot을 덮어써서 게시하지 마세요.
호스트의 `~/.codex`, `~/.claude`, `~/.paseo`, Docker socket은 mount하지 않습니다.

### Antigravity CLI

공식 `agy` CLI를 Go 설치기로 검증하여 설치합니다. Paseo에는 기존
`paseo-plugin-antigravity-cli`가 설치·활성화되어 있어야 합니다. 플러그인이 native
`stream-json`을 처리하고, 공용 Go launcher가 sandbox에서 `agy`를 실행합니다.
sbx 내장 agent에는 Antigravity가 없으므로 sandbox는 `shell`로 생성합니다.
인증은 sandbox 안의 공식 CLI에서 진행하며 호스트 Google 인증을 가져오지 않습니다.

```bash
make build
./paseo-sbx create --workspace /absolute/path/to/project \
  --profile antigravity-cli-native --build
./paseo-sbx shell antigravity-cli-native --workspace /absolute/path/to/project

# sandbox 안에서 공식 CLI를 실행하여 로그인
agy
```

공식 CLI의 인증 안내를 따라 로그인한 뒤 sandbox shell에서 나옵니다.
로그인 창과 callback 처리, Linux keyring 요구사항은 공식 CLI가 담당합니다.
Paseo 플러그인의 계정 화면은 호스트 계정을 관리하므로 sandbox 로그인에 사용하지 않습니다.
다음 명령으로 sandbox 프로파일을 지정한 Paseo 에이전트를 만듭니다.

```bash
./paseo-sbx run --workspace /absolute/path/to/project \
  --profile antigravity-cli-native '프로젝트를 확인해줘'
```

생성한 에이전트는 Paseo 앱에 표시됩니다.
`--model`, `--mode`, `--title`, `--paseo-home`을 지정할 수 있고,
`--dry-run`으로 daemon 요청 전에 에이전트 설정을 확인할 수 있습니다.
현재 Paseo 0.10.2는 플러그인 공급자의 config alias를 지원하지 않으므로
`register`는 Antigravity용 별도 `SBX …` 공급자를 추가하지 않습니다. 앱에서 일반
Antigravity 공급자를 직접 선택하면 호스트 CLI가 실행됩니다. SBX 에이전트는 위 `run`
명령으로 생성하고 이후 앱에서 사용합니다. daemon을 재시작하거나 플러그인 코드를 수정하지 않습니다.

방법론은 `antigravity-cli-superpowers`, `antigravity-cli-gsd-core`,
`antigravity-cli-ponytail`, `antigravity-cli-openspec` 중에서 선택합니다. Superpowers 스킬은
`~/.gemini/antigravity-cli/skills`에 설치합니다. GSD Core는 고정 버전의 공식
`--antigravity --global` 설치기를 사용하고 CLI config 경로를 명시합니다.
GSD가 생성하는 기본 경로 참조는 sandbox 안의 `~/.gemini/antigravity`를
`antigravity-cli`에 연결하여 해석합니다.
Ponytail은 고정 revision의 local checkout을 `agy plugin install`로 설치합니다.
RTK는 `--utilities rtk`로 선택합니다. Headroom의 Antigravity MCP 연동은 아직 구현하지 않았습니다.
첨부·schema·명령용 스킬 파일은 플러그인 data 디렉터리에서 임시 guest 디렉터리로 복사합니다.
workspace 밖의 일반 추가 디렉터리는 거부합니다. 플러그인의 모델 목록·호스트 전역 설정은
호스트 기준이며 sandbox 계정과 다를 수 있습니다. guest의 대화 기록 파일을 호스트에
공유하지 않으므로 해당 파일을 읽어야 하는 질문·background·subagent 추적 기능은 지원하지 않습니다.
Paseo MCP 공유 설정은 기본적으로 꺼 둡니다.

Gemini API key는 `sbx secret set google`과 sandbox 안의 공식 CLI 설정을 사용합니다.
sandbox에는 `GEMINI_API_KEY=proxy-managed` placeholder를 전달합니다.
CLI 인증·API 설정은 [Google 공식 문서](https://antigravity.google/docs/cli/install),
플러그인 옵션은 [Paseo provider options](https://paseo.sh/docs/sdk/provider-options.md)를 참고하세요.

## Paseo에서 실행과 자동 위임

Paseo 앱의 provider 선택에서 등록한 `SBX codex-superpowers` 등의 항목을 선택합니다.
작업 위치는 선택한 조합을 `create`로 등록한 workspace와 그 하위 디렉터리여야 합니다.
여러 프로젝트에서 사용하는 방법은 아래 "여러 프로젝트" 절과 [README_GUI.md](README_GUI.md)를
참고하세요. CLI로도 시작할 수 있습니다.

아래 위임 예제는 `codex-superpowers`와 `claude-ponytail`을 모두 선택하여 생성·등록한 경우입니다.

```bash
paseo run --provider codex-superpowers --cwd /absolute/path/to/project \
  '요구사항을 구현하고 독립적인 검토는 claude-ponytail에 위임해줘'
```

이미지의 전역 지침은 독립적인 작업을 Paseo MCP로 위임하는 흐름을 설명합니다.
모델은 `list_providers`, `list_models`로 실제 모델을 조회하고 `create_agent`로 하위
agent를 만듭니다. `provider`에는 `PROFILE/MODEL`을 쓰며 `title`과 `initialPrompt`를
전달합니다. `send_agent_prompt`, `get_agent_status`, `get_agent_activity`, `cancel_agent`,
`list_agents`로 자신과 생성한 하위 agent를 관리합니다. 실제 위임 여부는 작업의
독립성, 지침, 모델 판단에 따라 달라집니다.

세션별 호스트 relay가 Paseo의 MCP endpoint를 제한된 endpoint로 바꿉니다.
Paseo credential은 relay 프로세스 메모리에 유지하며 sandbox에는 임시 relay capability만
전달합니다. 호스트 터미널·스크립트·브라우저·스케줄 도구와 sandbox 외 provider는
relay에서 거부합니다. agent가 임의 workspace나 cwd를 지정하여 위임하는 것도 거부합니다.
위임할 수 있는 조합은 호출한 agent의 workspace에 등록된 조합으로 제한됩니다.
relay는 host loopback에만 bind하고 해당 sandbox에만 임시 포트 접근을 허용합니다.

호스트 launcher는 `~/.local/share/paseo-sbx/launcher/`에, workspace별 registry는
`~/.local/share/paseo-sbx/<workspace-id>/`에 저장합니다. agent가 host launcher와 registry를
수정하지 못하도록 `~/.local/share/paseo-sbx`를 포함하거나 그 안에 있는 workspace는 거부합니다.
같은 이유로 paseo-sbx 소스 디렉터리(또는 이를 포함하는 상위 디렉터리)는 workspace로
지정할 수 없으며, `create`와 `register`가 거부합니다. 저장소 안에서 `--workspace` 없이
TUI를 실행하면 현재 디렉터리가 workspace가 되므로 대상 프로젝트 경로를 지정하세요.
소스를 수정한 뒤에는 `register --apply`로 신뢰한 소스를 다시 설치합니다.

workspace 파일은 sandbox에서 쓸 수 있으므로 agent가 `.git/hooks`, `Makefile`,
`package.json` scripts 등을 변경할 수 있습니다. 호스트에서 `git`, `make`, `npm` 등을
실행하기 전에 변경 사항을 검토하세요. 이 경로는 workspace 쓰기 권한에 내재된 한계이며
paseo-sbx가 차단하지 않습니다.

## 동시 작업과 세션

선택한 sandbox들은 프로세스·에이전트 설정·세션이 독립적이며 프로젝트 파일은 공유합니다.
자동 위임 시 서로 다른 파일에 작업을 배정하세요. 초기 중계는 caller workspace에서만
하위 agent를 생성하므로, 에이전트가 Paseo worktree 생성 도구를 직접 호출하지 않습니다.
더 강한 파일 격리가 필요하면 호스트에서 별도 checkout/worktree를 만든 다음 그 경로를
새 workspace로 등록합니다.

Paseo가 host agent session 파일을 직접 읽는 일부 기능은 sandbox 내부 세션 경로에
접근할 수 없습니다. native plugin hooks의 trust는 설치와 별개이며 Codex의 `/hooks`
화면에서 필요한 hook을 검토합니다. Superpowers 6.4.2의 Codex plugin manifest에는
실행 hook이 없어 전역 AGENTS 지침에서 `using-superpowers` 호출을 안내합니다.
Ponytail과 GSD의 hook 설치 여부와 실제 실행 여부는 구분해야 합니다.

## 운영 명령

`shell`과 `stop`에는 해당 workspace의 registry에 있는 profile을 지정합니다.
아래 예제의 profile은 실제 생성한 조합으로 바꾸세요.

```bash
./paseo-sbx list --workspace /absolute/path/to/project
./paseo-sbx shell codex-superpowers --workspace /absolute/path/to/project
./paseo-sbx stop codex-superpowers --workspace /absolute/path/to/project

# 다른 프로젝트용 환경 선택·생성·등록
./paseo-sbx create --workspace /path/to/another-project
./paseo-sbx register --workspace /path/to/another-project --apply

# workspace에 저장된 조합과 utilities로 template 다시 빌드·로드
./paseo-sbx build --workspace /absolute/path/to/project
./paseo-sbx load --workspace /absolute/path/to/project

# 특정 조합만 빌드·로드할 때는 utilities도 동일하게 지정
./paseo-sbx build --profile codex-ponytail --utilities rtk
./paseo-sbx load --profile codex-ponytail --utilities rtk
```

sandbox 이름에는 workspace 경로 hash가 붙습니다. 다른 workspace의 sandbox는
삭제하지 않습니다. 기존 sandbox의 내용을 새 이미지로 자동 교체하지 않습니다.

### 여러 프로젝트

여러 workspace가 같은 조합을 사용할 수 있습니다. Paseo provider는 조합 이름으로 하나씩
등록되며 공용 launcher(`~/.local/share/paseo-sbx/launcher/agent-wrapper`)를 실행합니다.
launcher는 Paseo가 세션에 전달한 프로젝트 경로(`PASEO_AGENT_CWD`)를 포함하는 workspace의
sandbox로 연결합니다. 중첩된 workspace는 가장 깊은 쪽을 사용합니다.
프로젝트 경로가 해당 조합을 등록한 workspace에 속하지 않으면 실행을 거부합니다.
세션마다 별도 프로세스로 실행되므로 여러 프로젝트를 동시에 사용할 수 있습니다.

모델 조회처럼 세션 경로가 없는 Codex 실행은 해당 조합이 활성인 workspace 중
경로 순으로 첫 번째 sandbox를 사용합니다. 같은 조합의 sandbox는 모두 로그인해 두세요.

```bash
./paseo-sbx list --all                                   # 등록된 workspace와 조합
./paseo-sbx unregister --workspace /path/to/old-project  # 연결 대상에서 제외
./paseo-sbx prune --dry-run                              # 삭제된 workspace 확인
./paseo-sbx prune                                        # 삭제된 workspace의 기록 정리
```

`unregister`는 해당 workspace의 활성 조합을 비우고 Paseo provider 목록을 다시 만듭니다.
sandbox와 저장된 선택은 남기므로 `create --workspace <path> --skip-build --apply`로 다시
활성화할 수 있습니다.

폴더를 삭제한 workspace는 `list --all`에 `"missing": true`로 표시됩니다. 이런 workspace는
sandbox 연결과 Paseo provider 목록에서 자동으로 제외됩니다. `prune`은 그 기록
(`~/.local/share/paseo-sbx/<id>/`)을 삭제하고, Paseo 설정을 백업한 뒤 provider 목록을
다시 만들어 reload합니다. sandbox에는 로그인 상태와 세션이 남아 있으므로 삭제하지 않고
`sbx rm` 명령만 안내합니다. 외장 디스크처럼 잠시 연결이 끊긴 폴더도 삭제된 것으로
보이므로 먼저 `--dry-run`으로 대상을 확인하세요.
GUI 사용 절차는 [README_GUI.md](README_GUI.md)를 참고하세요.

## GitHub와 GHCR 재사용

소스와 lock 파일을 GitHub 저장소에 올립니다. `.generated/`, `.artifacts/`, tar 파일은
Git에서 제외됩니다. Workflow의 `publish`를 켜서 수동 실행하면 카탈로그에서 지원하는
조합의 이미지가 GHCR에 게시됩니다. 게시 옵션을 켜지 않으면 빌드만 수행합니다.

이미지 이름은 `ghcr.io/<owner>/<repository>/<profile>:2026.09.30-<config-hash>`입니다.
정확한 이름은 같은 선택으로 `build --print-bake`를 실행해 확인합니다.
CI는 카탈로그에서 지원하는 조합을 읽어 유틸리티 없는 이미지를 각각 빌드합니다.
현재 기본 카탈로그에는 열세 조합이 있습니다. utilities를 포함해 게시하려면 아래처럼
호스트 CLI의 `--utilities`로 지정합니다.

이미지는 `linux/amd64`, `linux/arm64` 두 플랫폼으로 빌드합니다. 호스트에서 직접
게시하려면 다음 명령을 사용합니다. `OWNER/REPOSITORY`는 실제 GHCR 경로로 바꾸세요.

```bash
./paseo-sbx build --profiles codex-superpowers --utilities rtk \
  --namespace ghcr.io/OWNER/REPOSITORY --platforms linux/amd64,linux/arm64 --push

# 같은 구성으로 실제 이미지 태그 확인
./paseo-sbx build --profiles codex-superpowers --utilities rtk \
  --namespace ghcr.io/OWNER/REPOSITORY --print-bake
```

게시 후 새 머신에서는 출력된 태그의 registry 이미지로 생성할 수 있습니다.

```bash
IMAGE='ghcr.io/OWNER/REPOSITORY/codex-superpowers:TAG_FROM_BAKE'
sbx create --name example --skills off --template "$IMAGE" codex /path/to/project
```

Paseo 자동 연결을 함께 구성하려면 출력된 태그의 이미지를 Docker에 가져와
같은 조합·utilities·namespace로 로드하고 생성한 뒤 등록합니다.

```bash
docker pull "$IMAGE"
./paseo-sbx load --profiles codex-superpowers --utilities rtk --namespace ghcr.io/OWNER/REPOSITORY
./paseo-sbx create --workspace /path/to/project --profiles codex-superpowers \
  --utilities rtk --namespace ghcr.io/OWNER/REPOSITORY --apply
```

위 두 생성 예제는 대안입니다. Paseo 연결을 사용할 경우 `paseo-sbx create` 예제를 사용합니다.
private registry 인증은 `sbx secret set --registry ghcr.io`로 설정합니다.

## 문제 확인

```bash
sbx ls
sbx template ls
sbx policy log
paseo provider ls
paseo provider diagnostic codex-superpowers
go test -race ./...
go vet ./...
```

모델 인증 부재는 설치 실패와 구별합니다. 모델 실행이 실패하면 먼저 credential 상태와
네트워크 정책을 확인하세요. 네트워크 문제를 해결할 때 전역 allow-all을 자동 적용하지 않습니다.
Paseo 설정을 되돌리려면 `~/.paseo/config.json.backup-*`에서 해당 백업을 복원하고 reload합니다.

GSD Core 1.15.0 공식 Codex installer는 일부 project-level `.claude/` 경로 참조에
경고를 출력합니다. Codex 전역 skills와 agent TOML은 공식 installer로 설치합니다.
이 경고가 특정 GSD workflow에 영향을 주면 upstream 수정 버전을 검토해 lock을 갱신합니다.

공식 문서: [SBX 설치](https://docs.docker.com/ai/sandboxes/install/),
[호스트 서비스 접근](https://docs.docker.com/ai/sandboxes/workflows/development/),
[Paseo MCP](https://paseo.sh/docs/mcp), [custom providers](https://paseo.sh/docs/custom-providers).

이번 머신의 구성 상태와 오류 해결 기록은 [구현 기록](docs/implementation-log.md)을 참고하세요.
