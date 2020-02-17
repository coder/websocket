;(() => {
  // expectingMessage is set to true
  // if the user has just submitted a message
  // and so we should scroll the next message into view when received.
  let expectingMessage = false
  function dial() {
    const conn = new WebSocket(`ws://${location.host}/subscribe`)

    conn.addEventListener("close", ev => {
      console.info("websocket disconnected, reconnecting in 1000ms", ev)
      setTimeout(dial, 1000)
    })
    conn.addEventListener("open", ev => {
      console.info("websocket connected")
    })

    // This is where we handle messages received.
    conn.addEventListener("message", ev => {
      if (typeof ev.data !== "string") {
        console.error("unexpected message type", typeof ev.data)
        return
      }
      const p = appendLog(ev.data)
      if (expectingMessage) {
        p.scrollIntoView()
        expectingMessage = false
      }
    })
  }
  dial()

  const messageLog = document.getElementById("message-log")
  const publishForm = document.getElementById("publish-form")
  const messageInput = document.getElementById("message-input")

  // appendLog appends the passed text to messageLog.
  function appendLog(text) {
    const p = document.createElement("p")
    // Adding a timestamp to each message makes the log easier to read.
    p.innerText = `${new Date().toLocaleTimeString()}: ${text}`
    messageLog.append(p)
    return p
  }
  appendLog("Submit a message to get started!")

  // onsubmit publishes the message from the user when the form is submitted.
  publishForm.onsubmit = ev => {
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
    })
  }
})()
