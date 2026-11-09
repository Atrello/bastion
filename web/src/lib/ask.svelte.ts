// Questions asked in the app's own dialog (components/Ask.svelte), never the browser's: "Delete this?",
// "Name of the new folder". ask() resolves with what was typed ('' for a plain yes), or null if cancelled.

export type Question = {
  title: string;
  message?: string;
  /** Ask for text: its label, and what's in the box to start with. */
  input?: { label: string; value?: string; placeholder?: string; required?: boolean; mustMatch?: string };
  /** The button that answers yes: "Delete", "Create" and so on. */
  confirm: string;
  /** For something that can't be undone: a red button. */
  danger?: boolean;
};

export const asking = $state<{ question?: Question; answer?: (value: string | null) => void }>({});

export function ask(question: Question): Promise<string | null> {
  asking.answer?.(null); // only one question at a time
  return new Promise((resolve) => {
    asking.question = question;
    asking.answer = (value) => {
      asking.question = undefined;
      asking.answer = undefined;
      resolve(value);
    };
  });
}

/** A yes-or-no question: true if they said yes. */
export const sure = async (title: string, message: string, confirm = 'Delete', danger = true) =>
  (await ask({ title, message, confirm, danger })) !== null;
