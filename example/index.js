;(() => {
  let conn
  let expectingMessage = false
  function dial() {
    conn = new WebSocket(`ws://${location.host}/subscribe`)

    conn.addEventListener("close", (ev) => {
      console.error("subscribe WebSocket closed", ev)
      console.info("reconnecting in 1000ms", ev)
      setTimeout(dial, 1000)
    })
    conn.addEventListener("message", ev => {
      if (typeof ev.data !== "string") {
        console.error("unexpected message type", typeof ev.data)
        return
      }
      appendLog(ev.data)
      if (expectingMessage) {
        messageLog.scrollTo(0, messageLog.scrollHeight)
        expectingMessage = false
      }
    })
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

  publishForm.onsubmit = async ev => {
    ev.preventDefault()

    const msg = messageInput.value
    if (msg === "") {
      return
    }
    messageInput.value = ""

    expectingMessage = true
    fetch("/publish", {
      method: "POST",
      body: msg,
    }).catch(err => {
      console.error("failed to publish", err)
    })
  }
})()
