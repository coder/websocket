;(() => {
  let conn
  let submitted = false
  function dial() {
    conn = new WebSocket(`ws://${location.host}/subscribe`)

    conn.addEventListener("close", () => {
      conn = undefined
      setTimeout(dial, 1000)
    })
    conn.addEventListener("message", ev => {
      if (typeof ev.data !== "string") {
        return
      }
      appendLog(ev.data)
      if (submitted) {
        messageLog.scrollTo(0, messageLog.scrollHeight)
        submitted = false
      }
    })

    return conn
  }
  dial()

  const messageLog = document.getElementById("message-log")
  const publishForm = document.getElementById("publish-form")
  const messageInput = document.getElementById("message-input")

  function appendLog(text) {
    const p = document.createElement("p")
    p.innerText = `${new Date().toLocaleTimeString()}: ${text}`
    messageLog.append(p)
  }
  appendLog("Submit a message to get started!")

  publishForm.onsubmit = ev => {
    ev.preventDefault()

    const msg = messageInput.value
    if (msg === "") {
      return
    }
    messageInput.value = ""

    submitted = true
    fetch("/publish", {
      method: "POST",
      body: msg,
    })
  }
})()
