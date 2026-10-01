# Antigravity의 기존 Paseo 플러그인 연동으로 전환

상태: Go-only 구현으로 전환. 기존 플러그인에 per-agent 실행 옵션을 전달한다.

## 문제와 확인한 사실

설치된 플러그인을 확인하기 전에 Antigravity 전용 연동이 없다고 가정하여
별도 ACP 서버와 인증 helper, QEMU 호환 실행을 추가했다. 자체 구현 경로의 테스트만
수행했으므로 이 잘못된 전제를 검증하지 못했다.

현재 local daemon은 Paseo 0.10.2이며, `paseo-plugin-antigravity-cli` 0.8.0이
running 상태다. `antigravity-cli`와 `omp` 공급자가 available 상태다.
플러그인은 공식 `agy`의 `stream-json`을 사용하고, 세션별 `providerOptions.agyPath`를
지원한다. 파일 기반 첨부·JSON schema·대화 기록·질문 처리도 포함한다.

현재 daemon의 ProviderOverridesSchema는 extends 대상으로 claude, codex, copilot,
opencode, pi, omp, acp만 허용한다. 플러그인 공급자 정의는 별도로 등록되므로
`extends: "antigravity-cli"`만 추가하면 작동한다는 전제도 성립하지 않는다.

## 변경 범위

1. 공식 `agy` 다운로드·checksum 검증은 기존 Go 설치 흐름에 통합한다.
2. 공용 Go launcher가 workspace에 맞는 sandbox에서 `agy`를 실행한다.
3. `paseo-sbx run`이 local daemon의 인증된 WebSocket API로 에이전트를 생성한다.
   `providerOptions.agyPath`와 `extraArgs`에 Go launcher와 SBX 프로파일을 지정한다.
   이벤트·세션·모델 변환은 설치된 플러그인을 재사용한다.
4. 첨부·schema·명령용 스킬은 plugin-data 경로에서 guest 임시 디렉터리로 복사한다.
   외부 일반 경로와 symlink는 거부한다. guest 대화 기록을 읽어야 하는 질문·background·
   subagent 추적 기능은 아직 지원하지 않는다. host 인증 폴더를 공유하지 않는다.
5. sandbox 안에서 공식 `agy`로 로그인한다. host의 플러그인 계정 화면은 host 계정용이며,
   sandbox의 인증과 구분한다. 실제 사용자 로그인이나 모델 호출은 자동 수행하지 않는다.
6. ACP 서버 artifact, ACP 인증 helper와 테스트, QEMU·amd64 호환 image, ACP 전용
   launcher 분기와 문서를 제거한다. Codex·Claude 기존 동작은 보존한다.

연결 구조:

```text
paseo-sbx run (Go)
  → local daemon API, provider = antigravity-cli
  → session.open의 agyPath를 공용 Go launcher로 지정
  → 기존 플러그인이 stream-json과 세션 이벤트 처리
  → Go launcher가 sandbox 실행·경로 검증 처리
```

## 공급자 목록과 언어

별도 SBX 공급자 alias를 추가하지 않는다. 생성된 에이전트는 기존 Antigravity 공급자로
앱에 표시된다. 앱에서 일반 Antigravity 공급자로 새 에이전트를 만들면 호스트 CLI가
실행되므로 SBX 실행은 `paseo-sbx run`으로 시작한다. TypeScript 코드·언어 예외는 필요 없다.
Codex·Claude 공급자 등록은 기존 방식으로 유지한다.

## 검증

Go race 테스트·vet·대상 아키텍처 빌드, native·Superpowers image 빌드가 통과했다.
시험용 Go agy를 통한 실제 플러그인 → launcher → sbx 응답과 plugin assets 복사를
확인했다. checksum 검증·첨부/schema 경로 변환·외부 경로 및 symlink 거부는 Go 테스트로
검증했다. 실제 Google 계정 로그인·모델 요청·guest 기록에 의존하는 resume/질문/background
기능은 검증하지 않았으며, 지원 한계는 README에 기록했다.
