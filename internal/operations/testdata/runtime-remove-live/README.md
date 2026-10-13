# Runtime remove live verification helper

2026-10-13 감독 승인 범위의 일회성 Go 실환경 검증 helper다. 제품 기능이나 상시 테스트가 아니다. HOME/home/CODEX_HOME을 재설정하지 않는다.

경로, 시험 이름, OrbStack socket, 기존 template, baseline 20개 조건이 해당 환경에 고정되어 있다. 다른 환경에서 그대로 실행하지 않는다. Go idle binary는 linux/arm64로 빌드한다. 기존 fixture root에서 prepare를 다시 실행하면 거부한다.

검증 root: `/private/tmp/paseo-sbx-runtime-remove-live-20261013`.
증거 root: `/private/tmp/paseo-sbx-runtime-remove-evidence`.

원본 checkout에서 helper를 빌드한다. 실행 전에 sbx/container/image/template baseline을 증거 root에 보존하고 감독의 실행 지시를 확보한다.

```sh
rtk proxy go build -o /private/tmp/paseo-sbx-runtime-remove-live-helper ./internal/operations/testdata/runtime-remove-live
rtk proxy /private/tmp/paseo-sbx-runtime-remove-live-helper prepare /Users/dev-r/devsbx/paseo-sbx/.worktrees/runtime-remove-fix
rtk proxy /private/tmp/paseo-sbx-runtime-remove-live-helper run /Users/dev-r/devsbx/paseo-sbx/.worktrees/runtime-remove-fix
```

prepare는 복사된 source root에 맞는 Go build overlay를 생성한다. 치환은 config.BaseDir 본문 하나뿐이다. 원본 CLI/remove/prune/backend는 변경하지 않는다. finish-prepare는 cross compile 환경 실패 이후 남은 준비만 재개한다.

run은 baseline drift를 먼저 검사하고 정확한 sandbox 두 이름/container 최대 두 CID/image exact ID를 ownership.json에 보존한다. 모든 외부 mutation은 동일 helper의 guard symlink를 통과한다. Paseo는 임시 home에만 접근한다. 실패하면 먼저 명령/관찰 상태를 남기고 cleanup을 시도한다. 명령별 timeout은 3분이다.

panic/timeout 또는 외부 강제 종료 이후에는 다음 명령으로 상태와 정리를 확인한다. guard 정책은 그대로 적용된다.

```sh
rtk proxy /private/tmp/paseo-sbx-runtime-remove-live-helper cleanup /Users/dev-r/devsbx/paseo-sbx/.worktrees/runtime-remove-fix
rtk proxy /private/tmp/paseo-sbx-runtime-remove-live-helper audit /Users/dev-r/devsbx/paseo-sbx/.worktrees/runtime-remove-fix
```

cleanup은 먼저 sbx/docker 상태를 보존하고 시험 sandbox의 workspace와 baseline 제외 여부를 확인한다. container는 정확한 시험 이름/image/labels/bind mount를 확인한다. create 직후 CID 기록 전 중단된 container만 이 확인을 거쳐 ownership에 보완한다. 기존 sandbox 20개와 기존 Docker 자원은 대상이 아니다. 시험 daemon stopped status, 최종 전체 목록/template/host hashes까지 재확인한다. 실패하면 보존된 ownership/로그로 판단하며 전역 삭제하지 않는다.

audit는 복사된 제품 파일과 원본 byte 일치를 manifest로 보존하고 제품 diff가 비었는지 확인한 뒤 cleanup을 호출한다. summarize는 final test JSON의 pass/fail을 집계한다.

최초 live run은 성공했다. 이후 감독 리뷰로 refusal 직후 별도 read-only running 관찰 assertion과 실패 cleanup을 추가했다. 후속 audit/cleanup은 실행했지만 최초 run을 다시 실행하지 않았다. 실패 자원이 남은 cleanup 분기의 고장 주입은 하지 않았다. 실제 실행 여부/제약은 결과 문서에 구분했다.

임시 source/overlay/ownership/workspace-remove sentinel은 증거로 남긴다. 생성한 sandbox/container/image와 시험 daemon은 정리됐다. 제품 생성·relay·TUI 수동 조작을 검증하는 helper는 아니다.
