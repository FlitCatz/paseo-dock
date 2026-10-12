# Sandbox 테스트 환경과 Docker 개발 환경 분리 설계

날짜: 2026-10-06
상태: 사용자 승인됨; Tasks 1–6 구현·독립 검토 완료, Task 7 실환경 검증 및 최종 branch 검토 진행 중

2026-10-08 검증에서 Docker/SBX provider 초기화·종료와 Claude 최소 이미지의 kit 권한 수정은
확인했다. 현재 Docker runtime의 Codex workspace-write sandbox 명령은 user namespace 권한
부재로 실패한다. 모델 호출·로그인은 별도로 미검증이다. 전체 완료 기준과 실환경 제한은
[Task 7 검증 기록](../reviews/2026-10-06-runtime-categories/task-7-report.md)에 기록한다.

이 문서는 `2026-09-30-paseo-sbx-design.md`의 SBX 전용 설치 배치와
`2026-10-02-management-tui-plan.md`의 sandbox 전용 생성 범위를 수정한다.
기존 문서의 구현·검증 기록은 유지한다.

## 목적과 범위

관리 TUI가 다루는 실행 대상을 `sandbox`와 `container` 두 category로 나눈다.
`sandbox`는 네트워크 제약이 있는 AI agent 자체의 테스트 환경이다. 새 sandbox에는
선택한 Codex 또는 Claude Code 등 지원 agent의 생성 시점 최신 안정 버전만 설치한다. 이 프로젝트의
Superpowers, GSD Core, Ponytail, OpenSpec, RTK, Headroom 등 methodology·utility를
추가하지 않는다. 여기서 “agent만”은 최소 실행에 필요한 OS·런타임과 agent CLI는
포함하되, 프로젝트가 관리하는 추가 개발 도구는 제외한다는 뜻이다.

`container`는 개발 작업을 위한 일반 Docker 컨테이너다. 기존 agent + methodology
한 가지 + 선택형 utility 설치 방식을 옮겨 유지한다. 기존의 고정 버전·revision
lock, 설치 manifest, 빌드 레시피와 업데이트 경로를 container에 적용한다. 같은
workspace에서 두 category를 동시에 사용할 수 있다. Docker 전체 자원을 탐색하거나
관리하는 일반 도구로 확장하지 않고, 이 프로젝트가 생성한 대상만 관리한다.

사용자가 밝힌 요구는 sandbox를 테스트 전용으로, container를 기존 설치형 개발
환경으로 쓰는 것이다. 세부 네트워크 요구가 확정되지 않았으므로 초기 기본값은
Docker bridge + 정상적인 outbound DNS/HTTPS, 포트 공개는 사용자가 지정한 경우에만
호스트 `127.0.0.1`에 bind하는 것으로 둔다. host network는 이번 범위에서 제외한다.

## 선택한 접근과 대안

공통 관리 화면·작업 큐·잠금·Paseo 등록 흐름 안에 category별 실행 backend를 둔다.
한쪽 경로의 명령이 다른쪽에 새지 않도록 대상 식별자, 이미지 레시피, 프로세스 실행,
상태 조회, 네트워크 중계 동작을 명시적으로 분리한다.

Docker 전용 화면·CLI를 따로 만드는 방법은 초기 구현이 빠르지만 workspace 선택,
버전 기록, Paseo provider 병합과 취소 처리를 복제한다. SBX를 완전히 교체하는 방법은
기존 테스트 환경과 저장 기록을 깨뜨린다. 두 대안은 채택하지 않는다.

## 사용자 흐름과 category 계약

생성 wizard 첫 단계에서 `sandbox: agent 테스트` 또는 `container: 개발 환경`을
선택한다. sandbox에서는 agent만 선택하며 methodology·utility 선택 화면을 보이지
않는다. container에서는 현재 agent/methodology 조합과 utility 선택을 제공한다.
최종 확인에는 category, 설치 구성, 이미지 출처·확정 버전, workspace mount,
네트워크 방식, 포트 매핑, Paseo 등록 여부를 표시한다. CLI의 create/build/list/
shell/stop/update/versions/register/unregister에도 category를 명시할 수 있다.
기존 `--profiles codex-superpowers`처럼 methodology가 포함된 create/build 호출은
container로 해석하여 기존 설치 구성을 보존한다. 새 agent-only 생성은
`--category sandbox --agent codex`처럼 명시한다. shell/stop/update 등 기존
대상 명령은 저장된 registry의 category로 해석한다. 같은 workspace에 두 category가
모두 있을 때 대상이 생략되거나 모호하면 자동으로 하나를 선택하지 않고
category·대상 지정을 요구한다.

관리 화면은 workspace 아래 두 category를 구분해 보여준다. 상태 조회는 SBX 목록과
Docker의 이 프로젝트 관리 컨테이너 목록을 별개로 읽는다. Docker daemon 실패가
SBX 조회를 막지 않고, SBX 실패가 Docker 작업을 막지 않는다. 읽기 조회는 guest를
시작하지 않는다. shell·version·update가 중지된 대상을 시작할 수 있다면 실행 전
이를 알린다. 자동 시작 없는 Docker shell이 필요한 경우에는 명시적 start를 먼저
요구한다. 중지와 버전 확인은 선택한 정확한 대상에만 적용한다.

## 설치 이미지와 “최신”의 의미

이미지 레시피를 두 경로로 분리한다.

- Sandbox 이미지: 생성 또는 명시적 재빌드 시 공식 agent 배포 경로에서 최신 안정
  버전을 조회하고 **실제 버전·패키지 출처를 기록한 뒤** 이미지에 설치한다.
  Codex와 Claude Code는 공식 패키지의 안정 채널을 기준으로 한다. Antigravity CLI
  등 다른 adapter는 신뢰할 수 있는 최신 배포 확인 경로가 검증된 경우에만
  agent-only 생성 선택지로 활성화한다.
  `latest` 문자열만 태그에 남기지 않는다. 조회 실패 시 임의의 구버전으로
  성공 처리하지 않는다. SBX template의 필수 런타임 요구는 유지하되, inherited
  methodology·utility가 없는지 확인한다. SBX가 요구하는 base image에 그러한
  도구가 포함돼 있으면 제거 가능성을 검증하거나 별도 최소 base를 사용한다.
  `sbx template load`와 `sbx create --skills off`는 이 category에서만 사용한다.
- Container 이미지: 현재 Dockerfile의 agent별 base, 설치 스크립트, 고정된
  methodology revision/version 및 utility lock을 유지한다. Docker Buildx로 만든
  OCI 이미지를 호스트 Docker store에서 직접 사용한다. SBX template load를
  거치지 않는다. 이미지 태그와 OCI label에 category·agent·methodology·utility·
  lock fingerprint를 넣어 재사용 검증에 사용한다.

두 category 모두 agent의 실제 설치 버전과 빌드 당시 출처를 이미지 manifest에
기록한다. sandbox의 “최신”은 생성·명시적 업데이트가 확인한 시점의 최신이며,
실행할 때마다 무조건 패키지를 갱신한다는 뜻이 아니다. sandbox update는 agent
버전만 확인·갱신한다. container update는 기존 agent/methodology/utility별
업데이트와 단계별 결과 기록을 유지한다. 현존 sandbox의 도구 설치를 자동으로
제거하거나 이미지를 교체하지 않는다.

## 저장 형식, 호환성, 대상 검증

신규 저장 레코드는 category, workspace, agent, 선택 설치 구성, image fingerprint,
SBX sandbox 이름 또는 Docker container ID/이름, 활성 상태를 포함한다. 저장 스키마를
버전 2로 올리고 고유 키를 `sandbox/<agent>`와
`container/<agent>-<methodology>`처럼 category까지 포함해 만든다. 기존 category
없는 registry·selection은 `legacy sandbox`로 읽고 기존 profile/provider ID를
유지한다. 조회 시 자동 재작성하지 않으며, 새 형식으로 기록할 때도 기존 레코드를
그대로 보존한다. 새 생성 wizard는 legacy의 도구 포함 sandbox 구성을 다시 만들지
않는다. 기존 대상의 조회·shell·stop·등록·해제는 계속 허용하고, 업데이트는 기존
방식대로 수행하되 legacy임을 표시한다. 삭제·교체·일괄 migration은 이번 범위 밖이다.

`Target`은 category와 확정된 resource identity를 포함한다. 작업 직전에 trusted
registry의 동일 항목을 다시 읽고 category, workspace, 이름/ID, 이미지, 선택 옵션이
변하지 않았는지 비교한다. Docker에서는 프로젝트 관리 label, 정확한 container
ID, image ID, workspace bind mount, 네트워크·포트 설정을 inspect로 확인한 후
재사용·변경한다. 이름이 같다는 이유만으로 외부 Docker 컨테이너를 조작하지 않는다.
동일 대상의 CLI·TUI·launcher 작업은 category가 포함된 lock key를 공유한다.

## Docker container 실행과 네트워크

container 생성은 선택된 workspace 하나를 동일 절대 경로에 읽기·쓰기 bind mount
하고, 컨테이너의 홈과 인증·세션 파일은 해당 컨테이너의 writable layer 또는
전용 managed volume에 보존한다. 호스트 홈 전체와 trusted runtime을 mount하지
않는다. 재시작 가능한 장기 실행 컨테이너로 만들고 `docker exec`로 shell·agent
실행·버전 확인·업데이트를 수행한다. `docker stop`은 정확한 관리 container ID에만
적용한다. 생성 뒤 mount·네트워크·포트 변경이 필요한 경우 기존 컨테이너를
암묵적으로 재생성하지 않고 불일치를 표시한다.

기본 네트워크는 Docker bridge다. 외부 패키지 저장소와 AI provider endpoint로의
정상적인 접속이 개발용 완료 기준이다. 개발 서버 포트는 명시된 host/container
포트 쌍만 공개하고 기본 bind 주소는 `127.0.0.1`로 한다. 포트 충돌·daemon 연결
실패·image 누락·mount 불일치는 실제 원인을 보여주고 registry를 성공 상태로
표시하지 않는다. workspace에서 실행하는 프로세스가 root 소유 파일을 만들지
않도록 기존 `agent` 사용자와 host 파일 권한 호환을 검증한다.

## Paseo provider와 세션 중계

새 provider ID는 `sbx-<agent>`와 `container-<agent>-<methodology>`로 구분한다.
legacy provider ID는 그대로 둔다. launcher는 provider ID를 trusted registry의
category 포함 Target으로 해석하고, SBX에는 `sbx exec`, container에는
`docker exec`를 사용한다. stdin/stdout 프로토콜, 현재 workspace, 종료 신호와
자식 프로세스 정리는 기존 계약을 유지한다. 호스트 Paseo 설정은 관리 provider만
병합하며 사용자 provider를 덮지 않는다.

Paseo MCP 중계의 bearer token은 호스트 메모리에 두고 guest에는 제한된 일회성
capability만 전달한다. SBX 경로는 기존 `sbx policy allow network`를 사용한다.
Docker 경로는 그 정책을 호출하지 않고 bridge container에서 중계 주소로 실제
접속 가능한지 검증한다. 현재 relay가 호스트 loopback에만 bind하는 구조이므로
`host.docker.internal`의 도달 가능성을 OrbStack 실환경에서 확인해야 한다.
도달하지 못하면 provider 등록을 성공으로 표시하지 않고, Docker 전용의 좁은
접속 경로를 설계·검증한 뒤 활성화한다. 호스트의 원본 Paseo credential을
컨테이너 환경변수나 mount로 전달하지 않는다.

## 구현 경계와 실패 처리

`internal/config`는 category-aware selection/registry와 legacy 읽기를 담당한다.
`internal/operations`는 공통 typed request, target 검증, 상태 snapshot, queue
이벤트와 category별 backend dispatch를 담당한다. SBX와 Docker backend는 각각
자신의 create/list/inspect/exec/stop을 소유한다. 공통 updater는 guest 명령 실행
transport를 받아 agent-only와 full-stack 단계를 명시적으로 선택한다.
`internal/launcher`, `internal/gateway`, `internal/tui`, `internal/cli`는 category를
보존한 식별자를 넘긴다. 새 실행 로직과 테스트는 Go로 작성한다.

도중 취소는 호스트 명령과 process group에 전달한다. guest 내부 설치 중단이
확실하지 않으면 `확인 필요`로 기록한다. 일부 컨테이너만 생성되거나 Paseo 등록만
실패해도 남은 resource와 재시도 경로를 표시한다. 자동 rollback, 삭제, 기존
컨테이너 덮어쓰기는 수행하지 않는다. 자동 상태 조회는 version/update를 호출하지
않으며, 불명 상태를 absent로 해석하지 않는다.

## 검증과 완료 기준

1. 기존 registry와 CLI 호출을 읽고 legacy sandbox를 기존 이름·provider ID로
   다루며, 기존 profile 기반 생성 호출은 container 설치 구성으로 해석한다.
   새 sandbox 생성에는 methodology·utility가 들어가지 않는다.
2. 새 sandbox 이미지에 생성 시점 확인된 agent 버전만 프로젝트 관리 설치물로
   존재한다. Codex와 Claude Code 각 한 개의 폐기용 sandbox에서 버전·shell·stop을
   확인한다. 외부 배포 조회 실패는 오류로 기록한다.
3. 각 선택 가능한 agent/methodology 조합의 container 빌드 명세가 기존 lock과
   설치기를 사용한다. 최소 한 조합의 폐기용 container에서 methodology와 선택
   utility의 실제 설치·버전을 확인한다.
4. 폐기용 Docker container에서 외부 DNS/HTTPS, workspace 읽기·쓰기,
   localhost에 공개한 개발 서버 포트, 재시작 후 홈·세션 보존, shell·stop·update·
   version을 확인한다. 관리 대상이 아닌 Docker 컨테이너는 변경하지 않는다.
5. 같은 workspace의 두 category를 동시에 조회·선택할 수 있고, 각 Paseo provider가
   올바른 runtime으로 실행되며 중계와 세션 종료가 실제로 동작한다. 로그인과 실제
   모델 호출은 별도 인증 조건으로 결과를 구분한다.
6. fake backend 상태 전이·대상 고정·취소·부분 실패·legacy migration과 PTY 흐름을
   검증한다. `make test`, `make vet`, `make build`를 통과한다.

현재 관리 TUI branch의 미완료 최종 리뷰 수정은 보존한다. 이 설계가 승인되면
기존 계획을 category-aware 작업 계획으로 개정한 뒤, 격리 worktree에서
작업별 subagent 구현·독립 리뷰를 이어간다.
