import { KeyRound, ShieldCheck } from "lucide-react";
import { ErrorNotice } from "./Feedback";

export function LegacyLogin({
  password,
  setPassword,
  busy,
  error,
  submit,
}: {
  password: string;
  setPassword: (value: string) => void;
  busy: boolean;
  error: unknown;
  submit: () => void;
}) {
  return (
    <div className="legacy-login">
      <div className="legacy-login-brand">
        <ShieldCheck aria-hidden="true" />
        <h1>绮梦影库</h1>
      </div>
      <form
        className="legacy-login-form"
        onSubmit={(event) => {
          event.preventDefault();
          submit();
        }}
      >
        <div className="legacy-login-title">
          <KeyRound aria-hidden="true" />
          <h2>登录</h2>
        </div>
        <input
          className="legacy-login-input"
          type="password"
          placeholder="管理密码"
          aria-label="管理密码"
          value={password}
          onChange={(event) => setPassword(event.target.value)}
          autoComplete="current-password"
          required
          autoFocus
          disabled={busy}
        />
        <ErrorNotice error={error} />
        <button
          className="legacy-login-button"
          type="submit"
          disabled={busy || !password}
        >
          {busy ? "登录中…" : "登录"}
        </button>
      </form>
    </div>
  );
}
