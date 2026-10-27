import { type KeyboardEvent, useEffect, useId, useRef, useState } from "react";
import { type ChatRow, type ChatSections, chatTexts, checkChatTitle } from "../agentView.js";
import { Button } from "../ui/Button.js";

// Chat titles come from the student (or the core's default): they reach the page as text nodes only.

export interface ChatBarProps {
  /** The open chat; null while no chat exists yet. */
  openChat: ChatRow | null;
  sections: ChatSections;
  /** The result of the last chat call, read out politely; "" when none. */
  note: string;
  onOpen(threadId: string): void;
  onNew(): void;
  /** Resolves to "" when renamed, else the text to show. */
  onRename(threadId: string, title: string): Promise<string>;
  onArchive(threadId: string): void;
  onRestore(threadId: string): void;
}

function RunMark() {
  return <span className="ag-chat-run small">{chatTexts.running}</span>;
}

function ChatItem(props: { row: ChatRow; open: boolean; onOpen(): void; onToggle(): void }) {
  const { row } = props;
  const action = row.archived ? chatTexts.restore : chatTexts.toArchive;
  return (
    <li role="none" className="ag-chat-row">
      <button type="button" role="menuitem" className="ag-chat-item" aria-current={props.open ? "true" : undefined} onClick={props.onOpen}>
        <span className="ag-chat-name">{row.title}</span>
        {row.running && <RunMark />}
      </button>
      <button type="button" role="menuitem" className="ag-chat-act small" aria-label={`${action}: ${row.title}`} onClick={props.onToggle}>
        {action}
      </button>
    </li>
  );
}

/** The chat part of the panel header: title, rename in place, archive, «Новый чат» and the list of chats as a menu. */
export function ChatBar(props: ChatBarProps) {
  const { openChat, sections } = props;
  const [menu, setMenu] = useState(false);
  const [showArchive, setShowArchive] = useState(false);
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState("");
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);
  const menuId = useId();
  const archiveId = useId();
  const root = useRef<HTMLDivElement>(null);
  const toggle = useRef<HTMLButtonElement>(null);
  const renameBtn = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLUListElement>(null);
  const field = useRef<HTMLInputElement>(null);
  const focusRename = useRef(false);

  const items = (): HTMLElement[] => Array.from(menuRef.current?.querySelectorAll<HTMLElement>('[role="menuitem"]') ?? []);

  // A menu opens with focus on the open chat (or the first item).
  useEffect(() => {
    if (menu) {
      const all = items();
      (all.find((el) => el.getAttribute("aria-current") === "true") ?? all[0])?.focus();
    }
  }, [menu]);

  useEffect(() => {
    if (editing) {
      field.current?.focus();
      field.current?.select();
    }
  }, [editing]);

  // Focus goes back to the rename button after the edit ends.
  useEffect(() => {
    if (!editing && focusRename.current) {
      focusRename.current = false;
      renameBtn.current?.focus();
    }
  }, [editing]);

  const closeMenu = (back: boolean) => {
    setMenu(false);
    if (back) {
      toggle.current?.focus();
    }
  };

  const onMenuKey = (e: KeyboardEvent) => {
    const all = items();
    const at = all.indexOf(document.activeElement as HTMLElement);
    if (e.key === "Escape") {
      e.preventDefault();
      e.stopPropagation();
      closeMenu(true);
    } else if (e.key === "ArrowDown" || e.key === "ArrowUp" || e.key === "Home" || e.key === "End") {
      e.preventDefault();
      const next = e.key === "Home" ? 0 : e.key === "End" ? all.length - 1 : (at + (e.key === "ArrowDown" ? 1 : -1) + all.length) % all.length;
      all[next]?.focus();
    }
  };

  const startEdit = () => {
    if (openChat === null) {
      return;
    }
    setDraft(openChat.title);
    setError("");
    setEditing(true);
  };

  const endEdit = () => {
    focusRename.current = true;
    setEditing(false);
    setError("");
  };

  const save = async () => {
    if (openChat === null || saving) {
      return;
    }
    const check = checkChatTitle(draft);
    if (!check.ok) {
      setError(check.text);
      return;
    }
    if (check.title === openChat.title) {
      endEdit();
      return;
    }
    setSaving(true);
    const problem = await props.onRename(openChat.threadId, check.title);
    setSaving(false);
    if (problem === "") {
      endEdit();
    } else {
      setError(problem);
      field.current?.focus();
    }
  };

  const pick = (id: string) => {
    props.onOpen(id);
    closeMenu(true);
  };

  const title = openChat?.title ?? chatTexts.newChat;
  const archivedCount = sections.archived.length;

  return (
    <div
      className="ag-chatbar"
      ref={root}
      onBlur={(e) => {
        // Focus left the whole control (a click elsewhere): the menu closes without stealing focus back.
        if (menu && !root.current?.contains(e.relatedTarget as Node | null)) {
          setMenu(false);
        }
      }}
    >
      <div className="ag-chat-line">
        {editing ? (
          <input
            ref={field}
            className="ag-inp ag-chat-edit"
            aria-label={chatTexts.renameLabel}
            aria-invalid={error !== "" || undefined}
            value={draft}
            disabled={saving}
            autoComplete="off"
            onChange={(e) => {
              setDraft(e.target.value);
              setError("");
            }}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.nativeEvent.isComposing && e.keyCode !== 229) {
                e.preventDefault();
                void save();
              } else if (e.key === "Escape") {
                e.preventDefault();
                e.stopPropagation();
                endEdit();
              }
            }}
            onBlur={() => {
              // Leaving the field cancels, unless a save is on its way.
              if (!saving) {
                setEditing(false);
                setError("");
              }
            }}
          />
        ) : (
          <>
            <button
              type="button"
              ref={toggle}
              className="ag-chat-toggle"
              aria-haspopup="menu"
              aria-expanded={menu}
              aria-controls={menu ? menuId : undefined}
              aria-label={`${chatTexts.chats}: ${title}`}
              title={title}
              onClick={() => (menu ? closeMenu(false) : setMenu(true))}
              onKeyDown={(e) => {
                if (e.key === "ArrowDown" && !menu) {
                  e.preventDefault();
                  setMenu(true);
                }
              }}
            >
              <span className="ag-chat-name">{title}</span>
              {openChat?.archived === true && <span className="ag-chat-tag small">{chatTexts.archivedMark}</span>}
              {openChat?.running === true && <RunMark />}
              <svg className="ag-chat-caret" viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                <path d="M6 9l6 6 6-6" />
              </svg>
            </button>
          </>
        )}
      </div>
      {!editing && (
        <div className="ag-chat-actions">
          <button type="button" ref={renameBtn} className="btn ghost sm" disabled={openChat === null} onClick={startEdit}>
            {chatTexts.rename}
          </button>
          {openChat !== null && (
            <Button
              size="sm"
              variant="ghost"
              onClick={() => (openChat.archived ? props.onRestore(openChat.threadId) : props.onArchive(openChat.threadId))}
            >
              {openChat.archived ? chatTexts.restore : chatTexts.toArchive}
            </Button>
          )}
          <Button size="sm" variant="secondary" onClick={props.onNew}>
            {chatTexts.newChat}
          </Button>
        </div>
      )}
      {menu && (
        <ul id={menuId} ref={menuRef} className="ag-chat-menu" role="menu" aria-label={chatTexts.list} onKeyDown={onMenuKey}>
          {sections.active.length === 0 && (
            <li role="none" className="ag-chat-empty small muted">
              {chatTexts.noChats}
            </li>
          )}
          {sections.active.map((r) => (
            <ChatItem
              key={r.threadId}
              row={r}
              open={r.threadId === openChat?.threadId}
              onOpen={() => pick(r.threadId)}
              onToggle={() => {
                props.onArchive(r.threadId);
                closeMenu(true);
              }}
            />
          ))}
          <li role="none" className="ag-chat-arch">
            <button
              type="button"
              role="menuitem"
              className="ag-chat-item ag-chat-archbtn"
              aria-expanded={showArchive}
              aria-controls={archiveId}
              onClick={() => setShowArchive((v) => !v)}
            >
              <span className="ag-chat-name">{chatTexts.archive}</span>
              <span className="small muted num">{archivedCount}</span>
            </button>
            {showArchive && (
              <ul id={archiveId} role="group" aria-label={chatTexts.archive} className="ag-chat-group">
                {archivedCount === 0 && (
                  <li role="none" className="ag-chat-empty small muted">
                    {chatTexts.emptyArchive}
                  </li>
                )}
                {sections.archived.map((r) => (
                  <ChatItem
                    key={r.threadId}
                    row={r}
                    open={r.threadId === openChat?.threadId}
                    onOpen={() => pick(r.threadId)}
                    onToggle={() => {
                      props.onRestore(r.threadId);
                      closeMenu(true);
                    }}
                  />
                ))}
              </ul>
            )}
          </li>
        </ul>
      )}
      <p className="ag-chat-note small" role="status" aria-live="polite">
        {error !== "" ? error : props.note}
      </p>
    </div>
  );
}
