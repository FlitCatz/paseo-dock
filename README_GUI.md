# Paseo GUI에서 사용하기

2026-10-12 기준으로 main에 통합된 sandbox/container 구성을 설명합니다.
설치·CLI·관리 TUI 전체는 [README](README.md), 구현 및 검증 상태는
[통합 기록](docs/superpowers/reviews/2026-10-12-pr-stack/README.md)을 참고하세요.
Paseo 앱의 메뉴 위치는 버전에 따라 다를 수 있습니다.

## 실행 대상과 provider

| 대상 | 설치 구성 | Provider ID 예시 |
| --- | --- | --- |
| 새 SBX sandbox | Codex 또는 Claude Code와 기본 런타임 | `sbx-codex`, `sbx-claude` |
| 새 Docker container | agent + methodology + 선택 utility | `container-codex-superpowers` |
| 기존 legacy SBX | 기존 조합과 저장된 설정 유지 | `codex-superpowers` |

등록 코드의 목록 label은 `SBX ` 뒤에 provider ID를 붙입니다. 따라서 새 Docker
조합은 `SBX container-codex-superpowers`, 새 agent-only sandbox는 `SBX sbx-codex`로
표시됩니다. 기존 `SBX codex-superpowers`는 legacy 기록에 해당합니다.
어느 workspace로 연결할지는 **세션의 프로젝트 폴더**로 결정됩니다. 같은 provider를
여러 프로젝트에 등록해도 목록 항목은 공유하며, 해당 프로젝트에 없는 조합은 거부됩니다.

## 프로젝트 준비

`--workspace`에는 프로젝트의 절대 경로를 지정합니다. 프로젝트가 paseo-sbx 소스나
신뢰된 runtime 파일을 포함해서는 안 됩니다.

```bash
cd /path/to/paseo-sbx
make build

# Agent-only sandbox
./bin/paseo-sbx create --workspace /path/to/A \
  --category sandbox --agent codex --build --apply

# Methodology를 포함하는 Docker 개발 환경
./bin/paseo-sbx create --workspace /path/to/B \
  --category container --profiles codex-superpowers,claude-superpowers \
  --utilities rtk --build --apply

# 새 Codex container에 검토된 user namespace 정책을 명시적으로 선택
./bin/paseo-sbx create --workspace /path/to/C \
  --category container --profile codex-superpowers --codex-userns --build --apply

./bin/paseo-sbx list --all
```

`--codex-userns` 기본값은 off입니다. 기존 container는 변환하지 않으며, 혼합 선택에서는
Codex container에만 적용합니다. 정책 범위와 플랫폼 제한은 [README](README.md)에 있습니다.
`--apply`는 기존 Paseo 설정을 백업한 뒤 provider 등록과 reload를 수행합니다.

터미널에서 `./bin/paseo-sbx`를 인수 없이 실행하면 관리 dashboard가 열립니다.
생성 wizard에서 category, agent와 해당 category의 설치 구성을 선택하고 최종 확인합니다.
Codex container의 최종 확인에서는 `u`로 user namespace opt-in을 바꿉니다.

## 로그인과 세션 시작

로그인은 실행할 runtime 안에서 진행합니다. 예를 들어:

```bash
./bin/paseo-sbx shell codex --category sandbox --workspace /path/to/A
./bin/paseo-sbx shell claude-superpowers --category container --workspace /path/to/B
```

각 agent의 공식 로그인 안내를 따르세요. SBX secret proxy의 credential은 Docker
container에 자동 전달되지 않습니다. 자세한 절차는 [README의 인증 안내](README.md#모델-인증)를
참고하세요. Docker 대상이 중지돼 있으면 shell/provider 실행 전에 명시적으로 시작해야 합니다.

Paseo에서 새 세션의 작업 폴더를 등록한 프로젝트 또는 그 하위 폴더로 지정하고,
그 프로젝트의 provider를 선택합니다. 예제에서는 A의 `sbx-codex`, B의
`container-codex-superpowers` 또는 `container-claude-superpowers`, C의
`container-codex-superpowers`를 선택합니다.

Antigravity native는 기존 plugin과 category별 provider 경로가 다릅니다.
생성·로그인·실행은 [Antigravity 안내](README.md#antigravity-cli)를 참고하고,
여러 category가 있으면 `--category`와 profile을 명시하세요.

## 등록 변경과 진단

```bash
./bin/paseo-sbx register --category container --workspace /path/to/B --apply
./bin/paseo-sbx unregister --category container --workspace /path/to/B
./bin/paseo-sbx list --all
./bin/paseo-sbx prune --dry-run
paseo provider diagnostic container-codex-superpowers
```

`unregister`는 연결에서 제외하며 runtime을 삭제하지 않습니다. 작업 폴더 경계 오류는
세션 폴더와 등록된 workspace를 확인하세요. 목록이 보이지 않으면 등록 결과와 Paseo의
reload 상태를 확인합니다. 모델 조회나 세션 인증 오류는 해당 runtime의 로그인을 확인합니다.
같은 workspace의 category가 겹치면 명령에 category를 지정합니다.

## 검증 범위

구현과 main 통합, race/helper 테스트·vet·build는 완료했습니다. 새로운 category/userns
경로의 **인증된 모델 추론과 모델이 실제 MCP/native 도구를 호출하는 인수 검증은 미완료**입니다.
이 안내는 사용 절차이며 실제 모델 실행 성공의 증거가 아닙니다. amd64 live confinement과
AppArmor 활성 Linux host도 아직 검증하지 않았습니다.
과거 legacy SBX의 모델·위임 기록은 새 category/userns의 검증으로 계산하지 않습니다.
