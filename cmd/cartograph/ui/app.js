// Operation forms stream their subprocess into the console pane instead of
// navigating away, so a long crawl reads as progress on the page that asked
// for it. Without scripting the same forms still work: the browser shows the
// raw text stream.
for (const form of document.querySelectorAll("form[data-op]")) {
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    const output = document.getElementById("op-output");
    const buttons = document.querySelectorAll("form[data-op] button");
    output.textContent = "";
    for (const button of buttons) button.disabled = true;
    try {
      const response = await fetch(form.action, {
        method: "POST",
        body: new URLSearchParams(new FormData(form)),
      });
      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        output.textContent += decoder.decode(value, { stream: true });
        output.scrollTop = output.scrollHeight;
      }
    } catch (error) {
      output.textContent += "\n" + error;
    } finally {
      for (const button of buttons) button.disabled = false;
    }
  });
}
