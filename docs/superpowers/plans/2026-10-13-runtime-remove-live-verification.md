# 4단계 실환경 삭제 검증 결과

날짜: 2026-10-13. 상태: 실환경 실행·검증·정리 및 감독 리뷰 완료.
checkout: /Users/dev-r/devsbx/paseo-sbx/.worktrees/runtime-remove-fix, branch feat/runtime-remove.
최종 검증 기록을 별도 commit으로 남긴다. push/merge 없음. 제품 코드 변경 없음.

## 실행 범위와 격리

감독이 준비안을 리뷰한 뒤 실행을 지시했다. 실제 생성량은 sandbox 2개/container 2개/image 1개다. 기존 sandbox는 최초 설계의 12개가 아니라 baseline의 **20개 전체**를 보존 대상으로 삼았다.

--paseo-home만으로는 registry가 격리되지 않는다. config.BaseDir 아래 전체 registry를 읽는 syncProviders는 launcher 설치와 per-workspace agent-wrapper 제거도 수행한다. HOME/home/CODEX_HOME을 변경하지 않고 아래 경로를 사용했다.

| 용도 | 실제 경로 |
|---|---|
| 시험 root | /private/tmp/paseo-sbx-runtime-remove-live-20261013 |
| 복사된 Service.Root | 시험 root의 source |
| registry/launcher base | 시험 root의 runtime-base |
| Paseo home | 시험 root의 paseo-home |
| 단일 삭제 workspace | 시험 root의 workspace-remove |
| prune workspace | 시험 root의 workspace-prune |
| 증거 root | /private/tmp/paseo-sbx-runtime-remove-evidence |

Go helper가 소스를 복사하고 AST로 복사본 internal/config/config.go의 **BaseDir 함수 본문만** 임시 절대 runtime base 반환으로 치환했다. go build -overlay가 복사된 파일 경로를 replacement에 연결한다. 삭제/prune/CLI/backend는 원본과 동일하다. PASEO_SBX_RUNTIME_DIR 제품 기능은 추가하지 않았다.

overlay.json/overlay.diff에 정확한 파일 매핑과 한 함수 치환을 남겼다. copied-source-hashes.json에서 복사한 모든 제품 파일과 원본 byte 일치를 확인했다. original-product.diff는 빈 파일이다. .generated는 복사된 Service.Root 아래에만 쓴다. registry 열거는 시험 workspace 최대 두 개로 제한했고 prune preview에 시험 missing workspace 하나만 포함되는지 확인했다.

Go guard가 모든 외부 mutation을 검사한다. sandbox는 정확한 시험 이름, Docker는 생성 후 보존한 exact CID, image 삭제는 exact image ID만 허용한다. Paseo는 정확한 시험 --home만 허용했다. Docker endpoint는 OrbStack socket이다. 기존 registry/launcher/Paseo config/실제 checkout .generated의 파일 경로·내용 hash가 전후 동일하다.

## baseline과 시험 fixture

sbx-before.json은 기존 sandbox 20개의 원본 JSON이다. paseo-codex-gsd-core-9e9c60e373만 running, 나머지는 stopped였다. docker-before.jsonl/images-before.jsonl은 OrbStack container/image 0개다. templates-before.json은 SBX template 원본이다. Docker Desktop daemon은 연결 불가였다.

기존 Docker image가 없어 감독이 Go Linux static idle binary와 FROM scratch 전용 image 생성을 허용했다. --pull=false --network none으로 빌드하여 pull하지 않았다. image는 삭제 시험 동안 보존을 확인하고 cleanup에서만 exact ID로 삭제했다.

| fixture | 정확한 identity |
|---|---|
| sandbox 단일 삭제 | paseo-remove-live-20261013-single |
| sandbox prune | paseo-remove-live-20261013-prune |
| container 단일 삭제 | 1f398c38952b045ddcadd4158d3b9f63534e52b67121bc7a280d41ea06203c4d |
| container prune | 02b90302bd68238d4b6f9fadb6647960c8d85e207674a0d86855223c1eb0f49d |
| 시험 image | sha256:cfed4828d2bd6aaf148515cdd67e287076367664eec7d20658f642843feb4334 |

sandbox는 기존 docker.io/paseo-sbx-category-live-20261007/sandbox/codex:2026.09.30-3b32788af33b template을 --pull never --skills off로 사용했다. sbx exec exact-name /bin/true로 running 상태를 만들었다.

Docker fixture는 원본 validateDockerEntry/Inspect/validateDockerPolicy 조건을 충족한다. bridge, canonical workspace source와 destination이 동일한 단일 writable bind mount, exact image/CID, managed/category/profile label, codex-userns-v1 정책·digest label, 원본 seccomp JSON, no-new-privileges, SYS_ADMIN drop을 적용했다. idle 프로세스는 host UID/GID 501:20으로 실행했다. 정책 완화 없음.

## 명령과 실행 결과

완전한 argv/exit code는 commands.jsonl, stdout/stderr는 command-NNN.log에 보존했다. 제품이 실행한 실제 backend 명령도 external-commands.jsonl에 기록했다. 아래 모든 사례는 실제 실행했다. 거부 사례의 exit 1은 기대한 결과다.

| 명령 기록 | 사례 | exit | 확인 결과 |
|---|---|---:|---|
| 020 | running sandbox remove | 1 | is running; stop first, registry/provider 불변 |
| 021 | running container remove | 1 | 같은 거부 사유, registry/provider 불변 |
| 022 → 023 | sandbox stop → remove | 0 / 0 | 단일 sandbox 삭제, 다른 container profile/identity 및 다른 workspace registry 보존 |
| 024 → 025 | image/container inspect | 0 / 0 | image와 다른 profile의 container 존재 |
| 026 → 027 | container stop → remove | 0 / 0 | 단일 CID 삭제, workspace sentinel/image 보존 |
| 029 | missing workspace prune dry-run | 0 | 시험 missing workspace 두 대상만 표시, running blocker 안내 |
| 030 | 양 대상 running prune | 1 | 전체 거부, registry/provider 불변 |
| 031 → 032 | sandbox만 stop → mixed prune | 0 / 1 | running container 때문에 전체 거부, registry 유지 |
| 033 → 034 → 035 | container stop → dry-run → prune | 0 / 0 / 0 | 두 backend 대상 삭제 및 해당 registry 정리 |
| 036 → 037 | cleanup 전 image inspect | 0 / 0 | 삭제/prune 이후 image 보존 |
| 038 → 040 | container/sandbox/template 조회 | 0 | container 0, 기존 sandbox 20개 전체 필드 동일, template 동일 |
| 041 → 042 | 시험 daemon stop → exact image rm | 0 / 0 | 승인된 cleanup 완료 |
| 052 | 시험 daemon 실제 status | 0 | localDaemon: stopped, pid/listen null |
| 053 → 056 | cleanup 재확인 | 0 | sandbox 20개 전체 필드 동일, container/image 0, template 동일 |

대표 CLI는 overlay 빌드의 remove --workspace W --profile sandbox/codex 또는 container/codex-native --paseo-home P --yes, stop --workspace W --profile 해당-profile, prune --remove-runtimes --dry-run --paseo-home P, prune --remove-runtimes --paseo-home P --yes다. W/P의 정확한 절대 경로는 commands.jsonl에 있다. helper 재현 명령은 testdata/runtime-remove-live/README.md에 기록했다.

## 보존 및 감독 독립 확인

helper는 기존 20개 sandbox를 이름만 비교하지 않고 **원본 JSON의 모든 필드**로 비교했다. sbx-before.json/sbx-after.json이 증거다. host-hashes-before.json/host-hashes-after.json도 동일하여 실제 registry/launcher/Paseo config/원본 .generated가 보존됐다. SBX template parsed JSON도 전체 동일하다.

감독도 별도로 조회하여 supervisor-sbx-after.json에 저장했다. jq sort_by(.name) 정규화 후 baseline과 전체 필드 비교가 true(exit 0)였다. 감독의 Docker container/image 조회도 각각 exit 0/빈 목록, host hash 파일 cmp는 exit 0이었다. 감독은 020/021 running 거부, 023/027 단일 삭제, 032 mixed blocker, 035 prune, 052 daemon stopped 로그를 직접 확인했다.

감독의 독립 template JSON 비교도 true였고, tests-final.jsonl jq 집계는 pass test event 614/fail 0/세 package pass였다. 감독은 overlay.diff가 BaseDir 함수 본문만 바꿨다는 점도 확인했다.

## 최종 원본 검증 및 환경 실패 구분

- 원본 go build ./...: exit 0, build-final.log.
- 원본 go vet ./...: exit 0, vet-final.log.
- 원본 go test -json ./internal/tui ./internal/operations ./internal/cli: exit 0, tests-final.jsonl 원본 보존. test/subtest pass event tui 176/operations 314/cli 124, 총 **614**, fail 0. tests-summary.json 참고.
- 최종 Go helper build/vet: exit 0. helper-vet-final.log 보존.
- overlay CLI build: exit 0, command 002.
- 실제 live helper run 및 후속 audit/cleanup: exit 0.

최초 sandbox 내부 테스트는 localhost bind 제한으로 실패했고 권한 상승 동일 테스트가 통과했다. Linux idle cross compile도 내부 sandbox에서 Go 표준 라이브러리 접근 오류(command 004 exit 1), 권한 상승 동일 명령에서는 성공(command 005 exit 0)했다. helper vet 최초 cache 접근 실패도 권한 상승 재실행 exit 0이었다. 제품 수정으로 우회하지 않았다. 기존 launcher /var symlink 관련 6개 실패는 이번에 전체 suite를 반복하지 않았으며 이전 main 재현 보고와 구분한다.

## 정리·제약·변경

sandbox 2개/container 2개는 remove/prune으로 없어졌고 시험 image는 보존 검증 후 exact ID로 삭제했다. 시험 daemon도 정확한 home에 대해서만 종료했고 실제 status가 stopped다. sbx rm --all, 전역 sbx prune, 기존 template/image 변경은 수행하지 않았다.

missing workspace를 만들기 위해 helper가 workspace-prune 디렉터리를 삭제했다(prune --remove-runtimes 시나리오 029–035는 실행했다). workspace-remove/sentinel, 임시 source/overlay/idle 입력/ownership/Paseo config/launcher root는 **재현 증거로 남긴다**. 실행 중인 시험 자원은 없다. 기존 running sandbox 1개는 그대로 보존했다. 증거 디렉터리와 테스트 로그는 삭제하지 않는다.

최종 helper에 panic 후 상태 보존→ownership 기반 cleanup, 강제 종료 후 수동 cleanup/audit 진입점을 추가했다. 성공 실행 후 실제 audit/cleanup도 실행하여 자원이 이미 없음을 확인했다. 자원이 남은 실패 cleanup 분기를 별도로 고장 주입해 실행하지는 않았다. cleanup inspect exit 1(048/049/051)은 이미 정리된 fixture의 not-found이며 최종 전체 목록으로 부재를 확인했다.

**제약:** runtime base 경로만 검증용 치환했다. fixture는 직접 생성/registry 등록했으며 제품 생성·relay·로그인 흐름은 이번 삭제 검증 대상이 아니다. TUI 직접 조작은 이번 live run에서 하지 않았고 원본 관련 tests로 검증했다. 거부 직후 별도 read-only 관찰 assertion은 감독 후속 리뷰로 helper에 추가했지만 이번 최초 live run에서 실행되지 않았다. external-commands.jsonl에서 command 020의 제품 backend 명령은 `sbx ls --json` 하나(15행, 대상 status running), command 021은 `docker inspect --type container <CID>` 하나(16행, State.Running=true)뿐이고 stop/rm은 없었으며, 첫 제품 stop은 command 022의 `sbx stop`(17행)이다. 최초 실행은 running 거부의 backend inspect 로그, registry/provider 불변, 후속 stop/remove 성공을 검증했다. 자원 상한을 지키기 위해 재생성하지 않았다.

이번 변경은 이 결과 문서와 일회성 Go helper/README뿐이다. 감독이 최종 파일 리뷰를 승인했으며 세 파일의 검증 기록을 별도 commit으로 남긴다.
