import { useCallback, useEffect, useRef, useState, type FormEvent, type KeyboardEvent } from "react";
import { IconSend } from "../components/icons";
import { Banner, EmptyState, Panel, SeverityBadge, StatusPill } from "../components/ui";
import { usePolling } from "../hooks/usePolling";
import { formatRelative } from "../lib/format";
import {
  analysisLabel,
  analysisTone,
  buildHistory,
  countBySeverity,
  MAX_QUESTION_CHARS,
  metricRows,
  SUGGESTED_QUESTIONS,
  validateQuestion,
  type ChatMessage,
} from "../lib/analysis";
import { api } from "../services/api";
import type { AIStatus, Finding, PerformanceReport } from "../types/api";

// Analysis enumerates processes, so it refreshes less often than live tiles.
const REFRESH_MS = 15_000;

export function Analysis() {
  const fetcher = useCallback(() => api.analysis(), []);
  const { data, error, lastUpdated } = usePolling(fetcher, REFRESH_MS);
  const report = data?.analysis;

  return (
    <div className="page analysis">
      {error && <Banner tone="warning">{error}</Banner>}
      {!report ? (
        !error && <p className="muted">Analyzing recent telemetry…</p>
      ) : (
        <>
          <div className="grid-2">
            <Summary report={report} lastUpdated={lastUpdated} />
            <Panel title="Recommendations">
              {report.recommendations.length === 0 ? (
                <EmptyState>Nothing to fix right now. Recommendations appear here when an issue is detected.</EmptyState>
              ) : (
                <ol className="recs">
                  {report.recommendations.map((r) => (
                    <li key={r}>{r}</li>
                  ))}
                </ol>
              )}
            </Panel>
          </div>
          <Panel title="Detected issues" aside={<span className="muted small num">{report.findings.length} found</span>}>
            <FindingList findings={report.findings} />
          </Panel>
        </>
      )}
      {data && <AskSentinel ai={data.ai} />}
    </div>
  );
}

function Summary({ report, lastUpdated }: { report: PerformanceReport; lastUpdated: number | null }) {
  const counts = countBySeverity(report.findings);
  const window = report.windowSeconds >= 60 ? `last ${Math.round(report.windowSeconds / 60)} min` : "collecting history";
  return (
    <Panel
      title="Performance summary"
      aside={
        <span className="muted small" title={lastUpdated ? new Date(lastUpdated).toLocaleString() : undefined}>
          {window}
          {lastUpdated && ` · updated ${formatRelative(new Date(lastUpdated).toISOString())}`}
        </span>
      }
    >
      <div className="analysis-status">
        <div className="analysis-pills">
          <StatusPill tone={analysisTone(report.status)}>{analysisLabel[report.status]}</StatusPill>
          {counts.critical > 0 && <span className="muted small num">{counts.critical} critical</span>}
          {counts.warning > 0 && <span className="muted small num">{counts.warning} warning</span>}
          {counts.info > 0 && <span className="muted small num">{counts.info} info</span>}
        </div>
        <p className="analysis-summary">{report.summary}</p>
      </div>
      <div className="analysis-metrics">
        {metricRows(report.metrics).map((m) => (
          <div key={m.label}>
            <span className="label">{m.label} avg</span>
            <span className={`value num ${m.avg === "Unavailable" ? "unavailable" : ""}`}>{m.avg}</span>
            <span className="label num">peak {m.peak}</span>
          </div>
        ))}
      </div>
      {report.notes.length > 0 && (
        <ul className="analysis-notes">
          {report.notes.map((n) => (
            <li key={n}>{n}</li>
          ))}
        </ul>
      )}
    </Panel>
  );
}

function FindingList({ findings }: { findings: Finding[] }) {
  if (findings.length === 0) {
    return <EmptyState>No performance issues detected in the recent data.</EmptyState>;
  }
  return (
    <ul className="alert-list">
      {findings.map((f) => (
        <li key={f.id} className={`alert-item finding sev-${f.severity}`}>
          <div className="alert-head">
            <SeverityBadge severity={f.severity} />
            <span className="alert-title">{f.title}</span>
          </div>
          <p className="finding-evidence">{f.evidence}</p>
          <p className="alert-reason">{f.explanation}</p>
          <div className="alert-meta">
            <span className="tag">{f.category}</span>
            {f.target && <span className="tag">{f.target}</span>}
          </div>
        </li>
      ))}
    </ul>
  );
}

function AskSentinel({ ai }: { ai: AIStatus }) {
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [input, setInput] = useState("");
  const [pending, setPending] = useState(false);
  const [inputError, setInputError] = useState<string | null>(null);
  const nextId = useRef(1);
  const logRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const el = logRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [messages, pending]);

  const ask = async (question: string) => {
    const problem = validateQuestion(question);
    if (problem) {
      setInputError(problem);
      return;
    }
    const q = question.trim();
    const history = buildHistory(messages);
    setInputError(null);
    setInput("");
    setPending(true);
    setMessages((m) => [...m, { id: nextId.current++, role: "user", content: q }]);
    try {
      const res = await api.ask(q, history);
      setMessages((m) => [...m, { id: nextId.current++, role: "assistant", content: res.answer }]);
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      setMessages((m) => [...m, { id: nextId.current++, role: "assistant", content: msg, error: true }]);
    } finally {
      setPending(false);
    }
  };

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    if (!pending) ask(input);
  };

  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      if (!pending) ask(input);
    }
  };

  return (
    <Panel
      title="Ask Sentinel"
      aside={
        ai.enabled ? (
          <span className="panel-aside-row">
            <span className="muted small">AI · {ai.model}</span>
            {messages.length > 0 && (
              <button type="button" className="btn small-btn" onClick={() => setMessages([])} disabled={pending}>
                Clear
              </button>
            )}
          </span>
        ) : undefined
      }
    >
      {!ai.enabled ? (
        <Banner tone="info">
          The AI assistant is off. {ai.reason} Everything else on this page is calculated locally and works without it.
        </Banner>
      ) : (
        <>
          <div className="chips" role="group" aria-label="Suggested questions">
            {SUGGESTED_QUESTIONS.map((q) => (
              <button key={q} type="button" className="chip" onClick={() => ask(q)} disabled={pending}>
                {q}
              </button>
            ))}
          </div>
          {(messages.length > 0 || pending) && (
            <div className="chat-log" ref={logRef} aria-live="polite">
              {messages.map((m) => (
                <div key={m.id} className={`chat-msg ${m.role} ${m.error ? "error" : ""}`}>
                  <div className="chat-who">{m.role === "user" ? "You" : m.error ? "Error" : "Sentinel"}</div>
                  <div className="chat-text">{m.content}</div>
                </div>
              ))}
              {pending && (
                <div className="chat-msg assistant">
                  <div className="chat-who">Sentinel</div>
                  <div className="chat-text muted">Looking at your PC's data…</div>
                </div>
              )}
            </div>
          )}
          <form className="chat-form" onSubmit={onSubmit}>
            <textarea
              value={input}
              onChange={(e) => {
                setInput(e.target.value);
                setInputError(null);
              }}
              onKeyDown={onKeyDown}
              placeholder="Ask about your PC's performance…"
              rows={2}
              maxLength={MAX_QUESTION_CHARS}
              aria-label="Question for Sentinel"
            />
            <button type="submit" className="btn primary btn-icon" disabled={pending || !input.trim()}>
              <IconSend />
              Ask
            </button>
          </form>
          <div className="chat-foot small">
            {inputError ? <span className="tone-text-critical">{inputError}</span> : <span />}
            <span className="muted">
              Answers use only PC Sentinel's measurements. Asking sends your question, the findings and usage figures, hardware names and
              top program names (no file paths or hostname) to Anthropic.
            </span>
          </div>
        </>
      )}
    </Panel>
  );
}
