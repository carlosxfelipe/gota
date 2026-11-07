// @ts-check

/**
 * @typedef {import("../codegen/dist/sdk").TodoResponse} TodoResponse
 * @typedef {import("../codegen/dist/sdk").CreateTodoRequest} CreateTodoRequest
 * @typedef {import("../codegen/dist/sdk").GotaClient} GotaClient
 */

/**
 * Runtime equivalent of createGotaClient() generated in examples/codegen/dist/sdk.ts
 * @param {string} [baseUrl=""]
 * @returns {GotaClient}
 */
function createGotaClient(baseUrl = "") {
  return {
    baseUrl,
    async fetch(endpoint, ...args) {
      const options = /** @type {any} */ (args[0] || {});
      const [method, pathTemplate] = endpoint.split(" ");
      let path = pathTemplate;

      if (options.params) {
        const keys = Object.keys(options.params).sort(
          (a, b) => b.length - a.length,
        );
        for (const k of keys) {
          const v = options.params[k];
          path = path.replace(":" + k, encodeURIComponent(String(v)));
          path = path.replace("*" + k, String(v));
        }
      }

      const url = baseUrl.replace(/\/$/, "") + path;
      const headers = new Headers(options.headers || {});
      let body = undefined;

      if (options.body !== undefined) {
        headers.set("Content-Type", "application/json");
        body = JSON.stringify(options.body);
      }

      const res = await fetch(url, { method, headers, body });
      const data = await res.json().catch(() => null);

      if (!res.ok) {
        const err = /** @type {any} */ (
          new Error(
            data?.message ||
              data?.error ||
              `HTTP ${res.status}: ${res.statusText}`,
          )
        );
        err.status = res.status;
        err.fields = data?.errors || null;
        throw err;
      }

      return data;
    },
  };
}

// @ts-ignore - Vue is loaded globally via CDN
const { createApp, ref, reactive, onMounted } = Vue;

createApp({
  setup() {
    /** @type {{ value: string }} */
    const baseUrl = ref("http://localhost:3000");
    /** @type {{ value: TodoResponse[] }} */
    const todos = ref([]);
    const loading = ref(false);
    const creating = ref(false);
    const connectionError = ref(false);
    /** @type {{ value: { message: string; fields?: Record<string, string> | null } | null }} */
    const createError = ref(null);
    /** @type {{ value: string | null }} */
    const lookupError = ref(null);
    /** @type {{ value: string }} */
    const searchId = ref("todo_1");
    /** @type {{ value: TodoResponse | null }} */
    const selectedTodo = ref(null);

    /** @type {CreateTodoRequest} */
    const form = reactive({
      title: "",
      priority: "normal",
    });

    const client = () => createGotaClient(baseUrl.value);

    async function loadTodos() {
      loading.value = true;
      connectionError.value = false;
      try {
        todos.value = await client().fetch("GET /todos");
      } catch {
        connectionError.value = true;
      } finally {
        loading.value = false;
      }
    }

    async function createTodo() {
      creating.value = true;
      createError.value = null;
      try {
        const created = await client().fetch("POST /todos", {
          body: {
            title: form.title,
            priority: form.priority,
          },
        });
        todos.value.push(created);
        selectedTodo.value = created;
        searchId.value = created.id;
        form.title = "";
        form.priority = "normal";
      } catch (/** @type {any} */ err) {
        createError.value = {
          message: err.message,
          fields: err.fields,
        };
      } finally {
        creating.value = false;
      }
    }

    /**
     * @param {string} id
     */
    async function fetchTodoById(id) {
      if (!id) return;
      lookupError.value = null;
      selectedTodo.value = null;
      try {
        selectedTodo.value = await client().fetch("GET /todos/:id", {
          params: { id: id.trim() },
        });
      } catch (/** @type {any} */ err) {
        lookupError.value = `${err.status || "Error"}: ${err.message}`;
      }
    }

    /**
     * @param {string} id
     */
    function selectFromList(id) {
      searchId.value = id;
      fetchTodoById(id);
    }

    /**
     * @param {string} priority
     */
    function priorityBadgeClass(priority) {
      switch (priority) {
        case "high":
          return "bg-rose-500/10 text-rose-600 dark:text-rose-400 border-rose-500/25";
        case "low":
          return "bg-slate-500/10 text-slate-600 dark:text-slate-400 border-slate-500/25";
        default:
          return "bg-sky-500/10 text-sky-600 dark:text-sky-400 border-sky-500/25";
      }
    }

    onMounted(loadTodos);

    return {
      baseUrl,
      todos,
      loading,
      creating,
      connectionError,
      createError,
      lookupError,
      searchId,
      selectedTodo,
      form,
      loadTodos,
      createTodo,
      fetchTodoById,
      selectFromList,
      priorityBadgeClass,
    };
  },
}).mount("#app");
