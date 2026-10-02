# 상태 기반 sandbox 관리 TUI 계획

날짜: 2026-10-02
상태: 제안. 관리 TUI 구현은 아직 시작하지 않았다.
기준 commit: `a8f6588` (origin/main에 push 완료)

## 판단

가능하며 이 프로젝트의 다음 UX 개선으로 적합하다. 현재 `internal/tui/wizard.go`는
Bubble Tea v2 기반 생성 선택 화면이다. 이를 workspace와 sandbox를 계속 관찰하고
작업을 선택하는 관리 앱으로 확장한다. 기존 CLI는 자동화·스크립트용으로 유지한다.

현재 sbx 자체에도 `sbx tui`가 있다. 그러나 이 프로젝트의 workspace별 활성 조합,
methodology·utility 선택, 이미지 구성 불일치, 업데이트 기록, Paseo provider 등록을
함께 다루려면 프로젝트 전용 관리 화면이 필요하다. Docker 전체 자원을 관리하는 기능은
이번 범위에 포함하지 않는다.

## 진입과 화면

터미널에서 인수 없이 `./paseo-sbx`를 실행하면 관리 화면을 연다.
명시적인 `./paseo-sbx tui`도 제공하며 `--workspace`로 초기 선택을 지정한다.
비대화형 환경의 인수 없는 실행은 기존 usage와 종료 코드를 유지한다.

```text
PASEO SBX      sbx: 연결됨    Paseo: 실행 중    최근 갱신: 3초 전
┌ Workspaces ─────────┬ Sandboxes ───────────────────────────────┐
│ > TJERP             │ profile            runtime   등록  갱신 │
│   superpowers-t     │ > codex-gsd-core    stopped   활성  성공 │
│   ponytail-t        │   claude-gsd-core   running   활성  실패 │
├─────────────────────┼─────────────────────────────────────────┤
│ 작업                │ 선택 항목 상세                          │
│ 새 sandbox 생성     │ workspace / sandbox / image             │
│ 업데이트 / shell    │ agent·methodology·utility의 확인된 버전  │
│ 중지 / Paseo 등록   │ 확인 시각 / 오류 / 가능한 작업          │
├─────────────────────┴─────────────────────────────────────────┤
│ 작업 로그: Codex 설치 → 검증 → 전환 / 경과 시간 / 결과        │
└───────────────────────────────────────────────────────────────┘
Tab 패널 이동 · ↑↓/jk 선택 · Enter 작업 메뉴 · / 검색 · ? 도움말
```

넓은 화면은 workspace·sandbox·상세/로그를 함께 표시한다. 80×24에서는 상세와 로그를
탭으로 전환하고 더 작은 화면에서는 한 패널씩 표시한다. 한글·ANSI 너비, NO_COLOR,
선택 항목과 스크롤 유지, 종료 후 터미널 복원을 지원한다. 키보드 사용만으로 모든
주요 작업을 수행할 수 있어야 한다.

## 상태 모델

- workspace: 경로 존재 여부, 저장된 선택, 활성 profile, Paseo 등록 상태를 표시한다.
- sandbox: registry의 이름으로 `sbx ls --json` 결과를 조인한다. running/stopped,
  존재하지 않음, 연결 실패로 확인 불가를 구분한다. sandbox 실행 상태를 모델 작업의
  실행 상태로 해석하지 않는다. 등록하지 않은 sandbox는 별도 보기에서 읽기만 제공한다.
- 설치 상태: 이미지 최초 버전과 `updates/*.json`의 마지막 확인 버전을 구분한다.
  확인 시각 없는 버전은 '미확인', 이전 기록은 '마지막 확인'으로 표시한다.
- 이미지: 선택 구성과 registry 이미지가 다르면 재사용 불가를 보여준다.
- 연결 상태: sbx·Paseo·Docker build 환경의 오류를 각각 표시한다. Docker daemon이
  불가해도 조회·shell 등 sbx가 독립적으로 수행할 수 있는 작업은 유지한다.

기본 3초 간격으로 registry와 `sbx ls --json`을 갱신한다. Paseo 상태는 더 느린 간격으로,
template 목록은 해당 탭 진입 또는 수동 갱신 시 읽는다. 한 조회의 중복 실행을 막고,
오래된 응답이 현재 선택을 덮지 않도록 요청 ID와 안정된 workspace/profile ID를 사용한다.
연결 실패 때는 마지막 성공 결과와 오류·확인 시각을 함께 보여준다.

자동 조회는 `sbx exec`나 패키지 네트워크 조회를 호출하지 않는다. 로컬 sbx help에서
확인한 대로 `exec`는 중지된 sandbox를 시작하기 때문이다. 사용자가 '버전 확인'이나
'업데이트'를 선택할 때만 실행하고, 중지된 sandbox가 시작된다는 점을 작업 설명에 표시한다.

## 1차 작업 범위

| 사용자 작업 | 처리 |
|---|---|
| workspace 추가 / sandbox 생성 | 경로 picker + 기존 agent·methodology·utility wizard |
| 업데이트 | 선택 sandbox 또는 workspace의 활성 조합 전체, 단계·결과 표시 |
| shell / 로그인 | sandbox shell로 터미널 전환, 종료 후 관리 화면으로 복귀 |
| sandbox 중지 | 선택 대상 표시, 실행 중 세션에 영향이 있는 작업임을 확인 |
| Paseo 등록 / 해제 | 대상과 설정 변경 범위 표시, 기존 backup·merge 로직 사용 |
| 이미지 빌드 / template 로드 | 고급 작업 메뉴, 선택 구성과 로그 표시 |
| 누락 workspace 정리 | dry-run 결과 표시 후 registry만 정리; sandbox는 보존 |
| 상태 / 버전 확인 | 자동 상태 갱신과 명시적 guest 버전 확인을 구분 |

일반 작업은 Enter로 여는 작업 메뉴에서 선택하고 `u` 업데이트, `s` shell,
`n` 생성, `r` 상태 갱신을 제공한다. 선택 상태에 따라 실행 불가능한 작업은 이유를 표시한다.
생성은 최종 확인에서 실제 조합·자원·Paseo 적용 여부를 보여준다. 화면을 움직이거나
새로고침해도 실행 대상으로 확정한 항목은 바뀌지 않는다.

새 모델 작업 실행, 모델 목록·인증 상태 조회, sandbox 삭제·재생성은 후속 범위다.
특히 현재 `run`은 Antigravity native plugin 전용이므로 모든 공급자 실행 화면으로
확장하려면 Paseo 연동을 따로 설계한다. 없는 `sbx start` 명령을 가정하지 않으며,
shell 진입과 실제 agent 실행은 별도 동작으로 취급한다.

## Go 구조와 라이브러리

- 기존 `charm.land/bubbletea/v2`를 유지한다. 현재 버전은 2.0.10이다.
- `charm.land/bubbles/v2`의 list/table, viewport, spinner, textinput/filepicker,
  help/key를 사용하고 `charm.land/lipgloss/v2`로 패널과 상태를 표현한다.
  추가할 정확한 버전은 구현 시작 시 Bubble Tea 2.0.10과의 호환성을 검증해 고정한다.
- `internal/operations`: CLI와 TUI가 함께 사용하는 typed request/result,
  상태 snapshot, context를 받는 실행 서비스와 작업 이벤트를 둔다.
- `internal/tui`: dashboard·패널·작업 메뉴·생성 wizard·로그 표시를 담당한다.
- CLI는 flag parsing 후 operations를 호출한다. TUI가 자신의 CLI 출력 문자열을
  재파싱하거나 CLI 전체를 재실행하지 않도록 한다.
- 기존 wizard는 확인 시 `tea.Quit`로 끝난다. standalone 생성에서는 이를 유지하고,
  dashboard에 포함할 때는 생성 선택 결과를 부모에게 반환하도록 분리한다.
- `sandboxupdate.Update`에는 외부 context와 단계 이벤트를 받을 경로를 추가한다.
  기존 launcher 호출 API와 자동 갱신 기본 정책은 유지한다.

대화형 shell은 Bubble Tea의 `ExecProcess`로 TUI를 일시 중단하고 터미널을 넘긴다.
build/load/create/update는 비동기 Cmd와 작업 이벤트로 실행하여 조회·로그 화면을 유지한다.
실제 전체 진행률을 모르는 작업은 spinner·현재 단계·경과 시간을 표시한다.

## 작업 실행과 취소

1차에서는 변경 작업을 하나씩 실행하고 다른 작업은 대기열에 둔다. 읽기 조회는 계속
가능하다. 동일 sandbox와 전역 Paseo 설정 변경은 CLI·launcher에서도 공유할 lock과
연계하여 충돌을 막는다. 작업별 stdout/stderr는 bounded log buffer로 보내며,
네트워크·subprocess 처리에서 TUI model을 직접 변경하지 않는다.

실행 전 대상 ID·선택·옵션을 고정하고 필요한 상태를 다시 검증한다. 작업 완료 또는
실패 후 snapshot을 다시 읽는다. 생성 일부 성공이나 OpenSpec 초기화 실패 등은
실제 남은 자원과 재시도 방법을 보여준다. 성공 로그만으로 전체 성공을 추정하지 않는다.

취소는 외부 context, 호스트 subprocess와 자식 process group 종료로 연결한다.
호스트 `sbx exec` 종료가 guest installer 종료를 보장한다고 가정하지 않는다.
guest 상태와 부분 설치를 재확인하고 '취소 요청', '중단 확인', '확인 필요'를 구분한다.
취소를 위해 sandbox 전체를 중지하거나 완료된 변경을 자동 rollback하지 않는다.
실행 작업이 있을 때 종료는 작업 중단 여부를 선택하는 화면을 제공한다.

## 구현 순서와 완료 기준

1. **공용 실행 계층**: 기존 CLI 동작을 유지하며 context·typed 상태·작업 이벤트를 분리한다.
   updater도 외부 취소를 받을 수 있게 한다. 기존 CLI/race 테스트를 통과해야 한다.
2. **조회 dashboard**: 인수 없는 터미널 진입, workspace/sandbox/상세 패널,
   검색·갱신·선택 유지·오류 상태를 구현한다. 이 단계의 기본 조회는 자원을 변경하지 않는다.
3. **일상 관리 작업**: 업데이트·중지·shell·Paseo 등록/해제, 작업 로그·대기열·취소를 연결한다.
   shell에서 돌아온 뒤 화면이 복구되고, 작업 중에도 상태·로그를 볼 수 있어야 한다.
4. **생성과 고급 작업**: 경로 선택·wizard를 dashboard 안에 통합하고 build/load,
   이미지 불일치 안내·누락 workspace 정리를 연결한다.
5. **검증·문서**: fake backend로 상태 전이·대상 고정·실패·취소를 검증한다.
   실제 PTY에서 shell 복귀·한글·NO_COLOR·작은 화면·로그 과다 출력을 확인한다.
   폐기용 sandbox에서 생성→업데이트→중지→shell→등록→해제를 검증한다.

전체 `make test`(race), `make vet`, `make build`와 기존 CLI 호환성을 완료 기준으로 삼는다.
로그 전체를 문자열 snapshot으로 고정하는 테스트보다 상태·실행 대상·실패 복구를 검증한다.
관리 TUI는 위 순서로 구현하되 1차 사용자 흐름은 4단계까지 완성한다.

## 근거

- 저장소: `go.mod`, `internal/tui/wizard.go`, `internal/cli/cli.go`,
  `internal/cli/paseo.go`, `internal/config/config.go`, `internal/sandboxupdate/update.go`.
- 로컬 sbx CLI: `sbx --help`, `sbx exec --help`, `sbx template ls --help`.
- [Charm v2 공식 안내](https://charm.land/blog/v2/).
- [Bubbles 공식 component 문서](https://github.com/charmbracelet/bubbles/blob/main/README.md).
- [Bubbles v2 호환성 안내](https://github.com/charmbracelet/bubbles/blob/main/UPGRADE_GUIDE_V2.md).
- [Bubble Tea 2.0.10 ExecProcess](https://pkg.go.dev/charm.land/bubbletea/v2@v2.0.10#ExecProcess).
