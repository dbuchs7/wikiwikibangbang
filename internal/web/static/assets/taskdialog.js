// The task of a linked item at its engine (ADR-0151), worked as oneself
// with one's own grant: the item's page shows who holds it and offers to
// take it on, give it back and complete it; completing it — from there,
// or by moving its card to the state of a completed task — opens the task
// dialog, which draws the task's form with the values it opens with and
// sends it to the engine. Only once the engine accepts does the item move.
// The engine decides whether one may; its refusal is shown in its words.
import { h } from "./dom.js";
import { get, post } from "./api.js";
import { t } from "./i18n.js";
import { engineOnly } from "./taskparts.js";
import { loadForm } from "./engineviews.js";

const pathOf = (it) => "/api/items/" + encodeURIComponent(it.id) + "/engine-task";
const errorBox = (err) => h("p", { class: "error", role: "alert", text: err instanceof Error ? err.message : String(err) });

// connectHint offers to connect one's account to the engine.
const connectHint = (got) => h("p", { class: "notice" }, t("enginetask.connect", { engine: got.engine.name }), " ",
  h("a", { href: "#/account", text: t("enginetask.connectLink") }));

// taskDialog completes the task of a linked item, moving it to state; it
// resolves to whether the engine completed it. A state other than the one
// of a completed task is the engine's alone.
export function taskDialog(it, state) {
  return new Promise((resolve) => {
    let done = false;
    let form = null;
    const body = h("div", { class: "task-dialog-body" }, h("p", { class: "muted", text: t("autobackup.loading") }));
    const status = h("div", { class: "status", role: "status" });
    const actions = h("div", { class: "actions-row" });
    const dialog = h("dialog", { class: "task-dialog", id: "task-dialog" },
      h("h2", { text: t("enginetask.title", { title: it.title || it.key }) }), body, status, actions);
    const close = () => {
      form?.destroy();
      dialog.close();
      dialog.remove();
      resolve(done);
    };
    dialog.addEventListener("cancel", (ev) => { ev.preventDefault(); close(); });
    const cancel = h("button", { type: "button", class: "secondary", id: "task-dialog-cancel", text: t("common.cancel"), onclick: close });
    actions.append(cancel);
    document.body.append(dialog);
    dialog.showModal();

    const send = (variables) => async (ev) => {
      const button = ev.currentTarget;
      button.disabled = true;
      status.replaceChildren(h("p", { class: "muted", text: t("enginetask.sending") }));
      try {
        await post(pathOf(it) + "/complete", { variables });
        done = true;
        close();
      } catch (err) {
        button.disabled = false;
        status.replaceChildren(errorBox(err));
      }
    };
    (async () => {
      let got;
      try {
        got = await get(pathOf(it));
      } catch (err) {
        body.replaceChildren(errorBox(err));
        return;
      }
      const app = h("a", { href: got.taskApp, target: "_blank", rel: "noopener noreferrer", text: t("enginetask.openApp", { engine: got.engine.name }) });
      if (!got.connected) return body.replaceChildren(connectHint(got));
      if (!got.open) return body.replaceChildren(h("p", { text: t("enginetask.notOpen", { engine: got.engine.name }) }));
      if (state && got.completed && state !== got.completed) {
        return body.replaceChildren(h("p", { text: t("enginetask.onlyEngine", { state, engine: got.engine.name }) }), h("p", {}, app));
      }
      if (!got.form) {
        body.replaceChildren(h("p", { text: t("enginetask.noForm", { name: got.task.name, engine: got.engine.name }) }));
        actions.prepend(h("button", { type: "button", class: "primary", id: "task-dialog-send", text: t("enginetask.complete"), onclick: send({}) }));
        return;
      }
      const Form = await loadForm();
      const canvas = h("div", { class: "pm-form-canvas task-form" });
      form = new Form({ container: canvas });
      const kinds = form.get("formFields");
      const own = engineOnly(got.form.schema, (type) => Boolean(type && kinds.get(type)));
      if (own.length) {
        form.destroy();
        form = null;
        return body.replaceChildren(h("p", { text: t("enginetask.engineForm", { fields: own.join(", "), engine: got.engine.name }) }), h("p", {}, app));
      }
      body.replaceChildren(canvas, h("p", { class: "muted" }, t("enginetask.asYou", { engine: got.engine.name }), " ", app));
      await form.importSchema(got.form.schema, got.values || {});
      actions.prepend(h("button", { type: "button", class: "primary", id: "task-dialog-send", text: t("enginetask.complete"), onclick: (ev) => {
        const { data, errors } = form.submit();
        if (errors && Object.keys(errors).length) return; // form-js marks the fields
        return send(data)(ev);
      } }));
    })();
  });
}

// taskPanel is the task of a linked item on its page: who holds it at the
// engine, take it on or give it back, complete it. reload shows the page
// anew after a change.
export function taskPanel(it, reload) {
  const panel = h("section", { class: "card engine-task", id: "engine-task" }, h("p", { class: "muted", text: t("autobackup.loading") }));
  const status = h("div", { class: "status" });
  const act = (fn) => async (ev) => {
    ev.currentTarget.disabled = true;
    try {
      await fn();
      reload();
    } catch (err) {
      status.replaceChildren(errorBox(err));
      ev.target.disabled = false;
    }
  };
  (async () => {
    let got;
    try {
      got = await get(pathOf(it));
    } catch (err) {
      panel.replaceChildren(errorBox(err));
      return;
    }
    const head = h("h2", { text: t("enginetask.panel", { engine: got.engine.name }) });
    const app = h("a", { href: got.taskApp, target: "_blank", rel: "noopener noreferrer", text: t("enginetask.openApp", { engine: got.engine.name }) });
    if (!got.connected) return panel.replaceChildren(head, connectHint(got), h("p", {}, app));
    if (!got.open) return panel.replaceChildren(head, h("p", { text: t("enginetask.notOpen", { engine: got.engine.name }) }));
    const who = got.mine ? t("enginetask.yours") : got.task.assignee ? t("enginetask.heldBy", { name: got.task.assignee })
      : got.task.groups?.length ? t("enginetask.offered", { groups: got.task.groups.join(", ") }) : t("enginetask.nobody");
    panel.replaceChildren(head,
      h("p", { id: "engine-task-holder", text: who }),
      h("p", { class: "actions-row" },
        got.mine
          ? h("button", { type: "button", class: "secondary", id: "engine-task-unclaim", text: t("enginetask.unclaim"), onclick: act(() => post(pathOf(it) + "/unclaim")) })
          : h("button", { type: "button", class: "secondary", id: "engine-task-claim", text: t("enginetask.claim"), onclick: act(() => post(pathOf(it) + "/claim")) }),
        h("button", { type: "button", class: "primary", id: "engine-task-complete", text: t("enginetask.completeOpen"), onclick: async () => {
          if (await taskDialog(it, got.completed)) reload();
        } }),
        " ", app),
      status);
  })();
  return panel;
}
