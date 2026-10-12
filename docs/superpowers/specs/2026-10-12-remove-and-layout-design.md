# 환경 삭제 작업과 lazydocker식 화면 정리 설계

날짜: 2026-10-12
상태: 승인 (2026-10-12) — 확인은 profile 이름 입력, 삭제는 stopped 대상만,
정리 + 남은 sandbox/container 삭제 포함
기준 commit: `973e4d1`

## 배경

2026-10-02 관리 TUI 계획은 sandbox 삭제·재생성을 후속 범위로 미뤘다. 그 결과
`prune`은 registry만 지우고 sandbox를 고아로 남기며, 사용자는 `sbx rm`을 직접
실행해야 한다. 화면은 계획서의 wireframe(Workspaces / Sandbox / 상세 / 로그를
2×2로 배치)에서 출발했고, 2026-10-12에 정한 "lazydocker UI 형태 차용" 기준이
반영되지 않았다.

범위는 이 프로젝트가 만든 대상(registry에 기록된 SBX sandbox와
`io.paseo-sbx.managed=true` label의 Docker container)으로 한정한다. 임의의 Docker
container·image·volume·network 관리는 범위 밖이다.

## A. 환경 삭제

### 사용자 흐름

1. 대상 행에서 `d` 또는 작업 메뉴의 "대상 삭제"를 고른다.
   대상이 stopped가 아니면 삭제를 비활성화하고 이유를 표시한다.
   - running: "먼저 중지(`x`) 필요"
   - 확인 불가(unknown): "상태 확인 불가 · 삭제 불가"
   - missing(resource 없음): "registry 항목만 정리"로 바꿔 허용한다.
2. 확인 화면에 다음을 표시한다.
   - category, workspace, profile, sandbox 이름 또는 container ID, image
   - **삭제되는 것:** sandbox/container와 그 안의 로그인 상태·세션·guest 파일,
     sandbox 범위 secret(SBX), registry 항목, 업데이트·버전 기록, 해당 Paseo provider
   - **보존되는 것:** workspace 폴더(bind mount 원본), image·template, 다른 profile
   - 되돌릴 수 없다는 문구
3. 확인은 profile 이름을 정확히 입력해야 진행한다. 일치하기 전에는 Enter가
   동작하지 않는다. Esc는 취소.
4. 작업 로그에 단계(Paseo 해제 → 삭제 → 삭제 확인 → registry 정리)를 표시한다.

### 실행 순서와 실패 처리

작업은 확정한 `Target` 하나에 대해 다음 순서로 수행한다.

1. entry lock 획득 후 `resolveTarget`으로 category·workspace·이름/ID·image가
   확인 시점과 같은지 재검증한다. 다르면 아무것도 하지 않고 실패한다.
2. backend `Inspect`로 상태를 다시 본다. running이거나 확인 불가이면 아무것도
   바꾸지 않고 실패한다(확인 이후 다시 시작된 경우 포함).
3. 대상이 active이면 registry `Active`에서 빼고 `syncProviders`로 provider를
   먼저 제거한다. Paseo가 사라질 환경으로 요청을 보내지 않게 하기 위해서다.
4. backend `Remove`를 호출한다. 어느 backend도 내부에서 stop을 하지 않는다.
   - SBX: `sbx rm --force <name>`. 비대화형 실행에 `--force`가 필요하다.
     `--force`는 사용 중 sandbox도 지우므로 2단계 stopped 확인이 그 안전장치다.
   - Docker: 저장된 container ID로 inspect해 관리 label·category를 확인하고
     running이 아님을 확인한 뒤 `docker rm <id>`(`--force` 없음).
     이름이나 label 검색으로 대상을 고르지 않는다.
5. 삭제를 관찰로 확인한다(SBX `ls --json`, Docker inspect의 not-found).
   조회 실패나 timeout은 삭제 성공으로 해석하지 않는다.
6. registry에서 profile 항목과 `updates/<entry>.json`·버전 기록을 지운다.
   entry lock 파일은 prune 리뷰(task-1 I1) 결정대로 삭제하지 않는다.

실패 처리:

- 3단계 이후 실패하면 자동 rollback하지 않는다. Paseo 해제 상태와 남은 resource,
  재시도 경로(`R` 고정 대상 재시도 또는 CLI)를 결과에 표시한다.
- 4단계에서 취소되면 `NeedsVerification`으로 기록한다.
- 5단계에서 resource가 남아 있으면 registry를 유지하고 실패로 끝낸다.
  다시 실행할 수 있어야 한다(이미 없는 resource는 성공으로 진행).

### 정리 + 남은 sandbox/container 삭제

`prune`의 기본 동작(registry만 정리, resource 보존)은 바꾸지 않는다. 새 선택지
"정리 + 남은 환경 삭제"를 TUI 고급 메뉴와 CLI(`prune --remove-runtimes`)에 둔다.

- 대상: missing workspace registry의 모든 profile. category별 backend로
  SBX sandbox는 `sbx rm`, container는 `docker rm`으로 지운다.
- 확인 화면에 workspace별 sandbox 이름·container ID 목록을 보여주고, 확인은
  `prune` 입력으로 받는다(여러 대상이므로 profile 이름 대신 고정 단어).
- running 또는 확인 불가 대상이 하나라도 있으면 전체를 실행하지 않고 해당
  대상을 표시한다. 사용자가 먼저 중지해야 한다. 이를 위해 missing workspace의
  sandbox/container도 `x` 중지를 허용한다(현재 TUI는 "workspace 경로 없음"으로
  막는다; operations의 경로 처리는 없는 경로를 이미 허용한다).
- 실행: 모든 entry lock을 잡은 뒤 대상을 재검증하고, provider 동기화 →
  resource 하나씩 삭제·확인 → 모두 사라진 workspace의 registry 정리 순서로 한다.
  일부 resource 삭제가 실패하면 그 workspace의 registry는 남겨 재시도할 수 있게 한다.
- 기존 prune 출력의 결함도 고친다: 남은 resource 안내가 `Sandbox` 이름만 모으고
  container ID를 빠뜨린다. container는 `docker rm <id>`로 안내한다.

### 코드 변경

- `operations.Backend`에 `Remove(context.Context, config.Entry) error` 추가,
  `SBXBackend`·`DockerBackend` 구현.
- `operations.KindRemove`("remove")와 `Execute` 분기. stop과 같은 lock·재검증
  경로를 쓰되 대상은 정확히 하나만 받는다.
- registry 갱신은 기존 `writeJSON`과 workspace lock을 쓴다.
- CLI: `paseo-sbx remove --workspace <path> --profile <key> [--yes]`.
  `--yes` 없이 TTY가 아니면 거부한다.
- CLI: `paseo-sbx prune --remove-runtimes [--dry-run] [--yes]`.
- TUI: `actions()`에 "대상 삭제" 추가, 상태별 비활성 이유, 이름 입력 확인.
  missing workspace에서 중지·삭제만 허용하고 나머지 작업은 지금처럼 막는다.
  고급 메뉴에 "정리 + 남은 환경 삭제" 추가.
- 미등록 sandbox 보기(`v`)는 계속 읽기 전용이다.

### 테스트

- fake backend로 순서 검증: provider 해제가 Remove보다 먼저 일어난다.
- running·unknown 대상은 provider·registry·resource를 건드리지 않고 거부한다.
- backend는 stop을 호출하지 않는다. SBX `rm --force`, Docker `rm`(no force) 인수 배열.
- 확인 후 registry가 바뀌면 거부하고 resource와 provider를 건드리지 않는다.
- Remove 실패·취소·삭제 후 조회 실패에서 registry가 유지된다.
- 이미 없는 resource를 재시도하면 registry 정리만 하고 성공한다.
- Docker: label·category 불일치 container는 지우지 않는다.
- prune --remove-runtimes: running 하나가 섞이면 전체 거부, sandbox와 container를
  각각 올바른 backend로 삭제, 일부 실패 시 해당 workspace registry 유지,
  기본 prune은 resource를 지우지 않음.
- TUI: 이름 입력이 틀리면 실행 안 함, 새로고침 중에도 대상이 고정된다.

## B. lazydocker식 화면

### 배치

```text
PASEO SBX   sbx: 연결됨  Paseo: 실행 중  Docker: 연결됨      15:04:05
┌ 1 Workspaces ──────────┐┌ 상세 ─ 로그 ─ 버전 ─ 설정 ──────────────────┐
│› TJERP            2/2  ││ Profile: codex-gsd-core                      │
│  superpowers-t    1/2  ││ Category: container   Runtime: running       │
│  ponytail-t  (missing) ││ Image: paseo-sbx/codex-gsd-core:…            │
└────────────────────────┘│ Paseo 등록: 활성                             │
┌ 2 Sandbox / Container ─┐│ 업데이트: 성공 (10-12 14:58)                 │
│› ● codex-gsd-core   C  ││ …                                            │
│  ○ claude-gsd-core  C  ││                                              │
│  ○ codex            S  ││                                              │
└────────────────────────┘│                                              │
┌ 3 Templates ───────────┐│                                              │
│  codex-gsd-core:…      ││                                              │
└────────────────────────┘└──────────────────────────────────────────────┘
u 업데이트 · s shell · x 중지 · d 삭제 · n 생성 · Enter 메뉴 · ? 도움말
```

- 왼쪽: 목록 패널을 세로로 쌓는다(Workspaces, Sandbox/Container, Templates).
  폭은 화면의 1/3, 최소 28열. 포커스된 패널이 남는 높이를 더 가진다.
- 오른쪽: 선택 항목의 탭 viewport(상세 / 로그 / 버전 / 설정). 작업 로그는 실행 중
  자동으로 로그 탭으로 바꾸지 않고 탭 제목에 진행 표시만 붙인다.
- 행 앞 기호: `●` running, `○` stopped, `?` 확인 불가, `✕` missing.
  끝의 `S`/`C`/`L`은 sandbox/container/legacy. NO_COLOR에서도 기호로 구분된다.
- 80×24 미만: 왼쪽 목록 하나와 오른쪽 탭 중 포커스된 쪽만 표시(기존 단일 패널 동작 유지).
- 확인·작업 메뉴는 전체 화면 대체가 아니라 가운데 popup으로 띄운다.

### 키

| 키 | 동작 | 비고 |
|---|---|---|
| `1` `2` `3` / Tab | 왼쪽 패널 이동 | lazydocker와 같은 번호 이동 |
| `[` `]` | 오른쪽 탭 이동 | 기존 ←→/hl 유지 |
| `j` `k` ↑↓ | 선택 | |
| Enter | 작업 메뉴 | |
| `u` `s` `n` `r` `/` `?` `q` | 기존 유지 | |
| `x` | 중지 | 새 단축키, 확인 필요 |
| `d` | 삭제 | 새 단축키, 이름 입력 확인 |
| `a` | 고급(build/load/prune) | 기존 유지 |
| `v` | 미등록 보기 | 기존 유지 |

`t`(template 탭)는 Templates 패널이 생기므로 `3`으로 대체하고 한 릴리스 동안
같은 동작의 별칭으로 남긴다.

### 코드 변경

- `internal/tui/panels.go`의 `View` 배치 분기와 `panel()`을 왼쪽 스택/오른쪽 탭
  구조로 바꾼다. `renderSection`·`sectionDimensions`는 재사용한다.
- `Dashboard.Focus`를 왼쪽 패널 index와 오른쪽 탭 index로 나눈다.
- `templateTab` 상태를 Templates 패널 포커스로 대체한다. template 조회는 지금처럼
  패널 진입이나 수동 갱신 때만 한다.
- popup 렌더러를 추가하고 `managementContent`를 그 안에 넣는다.

### 테스트

- PTY·golden: 120×40, 80×24, 60×20, NO_COLOR, 한글 이름 truncate.
- 포커스 이동, 탭 이동, popup 중 키 격리.
- 기존 `dashboard_test`·`final_review_test`의 선택 유지와 polling 계약 유지.

## 진행 순서

1. A의 operations·backend·CLI와 테스트 (화면 변경 없이 독립 review)
2. A의 TUI 작업 메뉴·확인 화면
3. B의 화면 배치와 키
4. 폐기용 sandbox/container로 실환경 삭제 확인: 삭제 대상만 사라지고 다른 profile과
   image·workspace 폴더가 남는지 확인한다. 기존 12개 sandbox는 건드리지 않는다.

## 범위 밖

- 여러 대상 일괄 삭제, `sbx rm --all`
- image·template 삭제(별도 설계 필요)
- 재생성(삭제 + 생성 한 번에), 로그인 상태 백업·이전
- 관리 대상이 아닌 Docker 자원
