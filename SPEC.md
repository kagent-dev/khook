## KAgent Hook Controller

- Create a Kubernetes event hook controller for Kagent (https://kagent.dev)
- The hook object should define the following fields:
  - Event type (e.g. pod restart, pod pending, OOM kill, probe failed)
  - The kagent agent to call when the event occurs
  - The prompt to pass to the agent with the event data
- The controller will listen to the events defined by deployed hook objects.
- When an event occurs - it will call the defined agent through the Kagent API passing:
  - Event name,time, the name of the involved Kubernetes resource and the defined prompt.
  - It will record the event data in the hook status and mark the event as "firing"
  - If the same event occurs within 10 minutes - it will be ignored.
  - After 10 minutes the event will be cleared from the hook status as resolved.
  - If it occurs again after the 10 minute timeout - the hook will fire again.
