// Content of the help screen «Как увидеть события в Apple Calendar» (PRD section 11.3). Steps and labels come only
// from docs/refs/apple-calendar.md; paths the refs mark "not verified" point to the official page instead. Links are
// keys of the closed set in help.go: the core owns the URLs and opens them in the browser.

// The .js extension lets the Node test runner load the compiled module; TypeScript and Vite map it to the .ts file.
import { helpErrors, type HelpApi, type HelpPageKey } from "./bindings/help.js";

export interface HelpLink {
  key: HelpPageKey;
  caption: string;
}

/** A part of an option, rendered in this order: title, text, points, steps, note, links. */
export interface HelpBlock {
  id: string;
  title?: string;
  text?: string;
  points?: readonly string[];
  steps?: readonly string[];
  note?: string;
  links?: readonly HelpLink[];
}

export type Verdict = "recommended" | "fallback" | "notRecommended";

export interface HelpOption {
  /** The option of PRD section 11.3. */
  id: "A" | "B" | "C";
  label: string;
  title: string;
  verdict: Verdict;
  summary: string;
  blocks: readonly HelpBlock[];
}

export const verdictTexts: Readonly<Record<Verdict, string>> = {
  recommended: "Рекомендуется",
  fallback: "Запасной вариант",
  notRecommended: "Не рекомендуется",
};

export const helpTexts = {
  lead: "Подключения",
  title: "Как увидеть события в Apple Calendar",
  back: "Назад",
  intro:
    "Приложение записывает занятия и сроки заданий в свой отдельный календарь Google — календарь приложения. " +
    "Приложение создаёт его само после входа в Google в разделе «Подключения». Ниже — как увидеть этот календарь " +
    "на iPhone, iPad и Mac.",
  editTitle: "Расписание меняйте в приложении.",
  editText:
    "Изменения в Календаре Apple или в Google Календаре в приложение не попадают. Главная копия — в приложении, " +
    "и следующая синхронизация может перезаписать такие изменения.",
  troubleTitle: "Если событий не видно",
  entryTitle: "Календарь на iPhone, iPad и Mac",
  entryText: "Календарь приложения можно смотреть в Календаре Apple. Пароль от Apple ID не нужен.",
  entryButton: "Как увидеть события в Apple Calendar",
} as const;

/** The note next to a row of link buttons. */
export function linksNote(count: number): string {
  return count === 1 ? "Открывается в браузере" : "Открываются в браузере";
}

export const helpOptions: readonly HelpOption[] = [
  {
    id: "A",
    label: "Способ 1",
    title: "Через аккаунт Google",
    verdict: "recommended",
    summary:
      "Добавьте аккаунт Google на устройство Apple: Календарь Apple покажет календари этого аккаунта, в том числе " +
      "календарь приложения.",
    blocks: [
      {
        id: "a-iphone",
        title: "iPhone и iPad",
        text: "Где добавляется аккаунт, зависит от версии iOS:",
        points: [
          "iOS 18 и новее: настройки приложений собраны в «Настройки» → «Приложения». Точный путь к учетным записям " +
            "Календаря — на странице Apple.",
          "Более ранние iOS: «Настройки» → «Календарь» → «Учетные записи».",
        ],
        steps: [
          "Выберите «Google» и войдите в тот же аккаунт Google, что и в приложении.",
          "Включите «Календари». Почту и контакты можно выключить, если они не нужны.",
        ],
        note: "Если аккаунт Google уже добавлен, достаточно включить для него «Календари».",
        links: [
          { key: "apple-iphone-accounts", caption: "Как добавить учетную запись календаря на iPhone" },
          { key: "google-apple-sync", caption: "Google Календарь в Календаре Apple" },
        ],
      },
      {
        id: "a-mac",
        title: "Mac",
        text: "Аккаунт добавляется в одном из мест:",
        points: [
          "В приложении «Календарь»: меню «Календарь» → «Добавить учетную запись» → «Google» → «Продолжить», " +
            "дальше — по подсказкам.",
          "«Системные настройки» → «Учетные записи интернета» → «Добавить учетную запись» → Google. Затем выберите " +
            "приложения, которые будут использовать аккаунт, — среди них должен быть Календарь.",
          "В старых версиях macOS: «Календарь» → «Настройки» → «Учетные записи» → «Добавить» → «Google» → " +
            "«Продолжить».",
        ],
        note:
          "Затем поставьте флажок у календаря приложения в списке календарей. Если списка не видно: «Вид» → " +
          "«Показать список календарей».",
        links: [
          { key: "apple-mac-calendar-accounts", caption: "Добавление учетной записи в Календаре на Mac" },
          { key: "apple-mac-internet-accounts", caption: "Учетные записи интернета на Mac" },
        ],
      },
      {
        id: "a-missing",
        title: "Если календаря приложения нет в списке",
        text:
          "По умолчанию на устройства Apple попадают только календари из раздела «Мои календари» Google Календаря. " +
          "Остальные включаются на странице синхронизации Google.",
        steps: [
          "Откройте страницу синхронизации Google и войдите в тот же аккаунт.",
          "Отметьте календарь приложения и нажмите «Сохранить».",
          "Обновите Календарь на устройстве и проверьте, что календарь приложения включён в списке календарей. " +
            "На Mac это флажок у календаря.",
        ],
        links: [{ key: "google-syncselect", caption: "Страница синхронизации Google Календаря" }],
      },
      {
        id: "a-timing",
        title: "Когда появятся события",
        text: "События появляются не сразу.",
        points: [
          "iPhone и iPad: это может занять некоторое время, календарь обновляется сам.",
          "Mac: обновить вручную — «Вид» → «Обновить календари». Как часто обновлять, задаётся в «Календарь» → " +
            "«Настройки» → «Учетные записи» → аккаунт → «Обновить календари». С вариантом Push изменения приходят " +
            "сами.",
        ],
      },
    ],
  },
  {
    id: "B",
    label: "Способ 2",
    title: "Подписка по закрытой ссылке",
    verdict: "fallback",
    summary: "Только для просмотра: события видны, но изменить их на устройстве нельзя.",
    blocks: [
      {
        id: "b-link",
        title: "Где взять ссылку",
        steps: [
          "Откройте Google Календарь в браузере на компьютере.",
          "Нажмите значок настроек → «Настройки».",
          "В разделе «Настройки моих календарей» выберите календарь приложения.",
          "Откройте «Интеграция календаря» и скопируйте «Закрытый адрес в формате iCal».",
        ],
        links: [{ key: "google-secret-ical", caption: "Закрытый адрес календаря в формате iCal" }],
      },
      {
        id: "b-mac",
        title: "Подписка на Mac",
        steps: [
          "В приложении «Календарь» выберите «Файл» → «Новая подписка на календарь».",
          "Вставьте ссылку и нажмите «Подписаться».",
          "Укажите «Имя» и «Расположение»: iCloud — подписка будет на всех ваших устройствах, «На моем Mac» — " +
            "только на этом Mac.",
          "Выберите частоту в «Автообновление» и нажмите «OK».",
        ],
        note: "Частоту можно изменить позже: Control-клик по календарю → «Свойства» → «Автообновление».",
        links: [{ key: "apple-mac-subscribe", caption: "Подписка на календарь на Mac" }],
      },
      {
        id: "b-iphone",
        title: "Подписка на iPhone и iPad",
        text:
          "Как подписаться на календарь на iPhone и iPad, зависит от версии iOS — смотрите страницу Apple. Если при " +
          "подписке на Mac выбрать iCloud, подписка будет и на других ваших устройствах Apple.",
        links: [{ key: "apple-iphone-accounts", caption: "Как добавить учетную запись календаря на iPhone" }],
      },
      {
        id: "b-warn",
        title: "Важно",
        points: [
          "Никому не передавайте ссылку: по ней любой человек увидит события.",
          "Если ссылка попала к кому-то, нажмите «Сбросить» там же, где её скопировали. Старая ссылка перестанет " +
            "работать.",
          "В школьном или рабочем аккаунте ссылки может не быть: это настраивает администратор. Обратитесь к нему.",
          "Google не сообщает, как часто обновляется календарь по такой ссылке.",
          "Совет: выберите один способ — 1 или 2. Если включить оба, события, скорее всего, будут показаны дважды.",
        ],
      },
    ],
  },
  {
    id: "C",
    label: "Способ 3",
    title: "Сделать календарь общедоступным",
    verdict: "notRecommended",
    summary:
      "Тогда календарь может найти и посмотреть кто угодно, а поисковые системы могут его проиндексировать.",
    blocks: [
      {
        id: "c-app",
        points: [
          "Приложение никогда не делает календарь общедоступным.",
          "Приложение не просит пароль от Apple ID и не обращается к Apple.",
        ],
      },
    ],
  },
];

export const troublePoints: readonly string[] = [
  "На устройстве добавлен другой аккаунт Google — не тот, через который выполнен вход в приложении.",
  "Для аккаунта Google на устройстве выключены «Календари». На Mac аккаунт проверяется в «Учетные записи интернета».",
  "Календарь приложения не отмечен на странице синхронизации Google.",
  "Календарь приложения выключен в списке календарей.",
  "На Mac: проверьте часовой пояс и «Дата и время» в системных настройках.",
  "Школьный или рабочий аккаунт: администратор мог отключить закрытую ссылку и общий доступ.",
  "Старая iOS и двухэтапная аутентификация в Google: вместо обычного пароля нужен пароль приложения.",
];

/** Every link of the screen, in the order shown. */
export function helpLinks(): HelpLink[] {
  return helpOptions.flatMap((o) => o.blocks.flatMap((b) => b.links ?? []));
}

const knownErrors: readonly string[] = Object.values(helpErrors);

/**
 * The text of a failed open. The core's errors are fixed Russian sentences and are shown as is; anything else (an
 * error of the runtime, which may name internals) gets the generic text.
 */
export function openErrorText(err: unknown): string {
  const msg = errorMessage(err);
  return knownErrors.includes(msg) ? msg : helpErrors.failed;
}

/** Opens the page of the key; resolves to "" when the browser was asked to open it, or to the text to show. */
export async function openHelpLink(api: HelpApi, key: HelpPageKey): Promise<string> {
  try {
    await api.openHelpPage(key);
    return "";
  } catch (err) {
    return openErrorText(err);
  }
}

// Wails rejects a call with an Error-like object carrying the Go error text.
function errorMessage(err: unknown): string {
  if (typeof err === "string") {
    return err;
  }
  if (typeof err === "object" && err !== null && typeof (err as { message?: unknown }).message === "string") {
    return (err as { message: string }).message;
  }
  return "";
}
