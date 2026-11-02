# Protected content and access requests

Part of a page can be protected: some words, a paragraph, a heading with its section, a picture, a table, a diagram ([ADR-0088](../adr/0088-protected-passages-of-a-body.md), [ADR-0160](../adr/0160-protected-blocks-and-pictures-of-a-page.md)). Only the person who protected it and the people and groups they chose read it. Everyone else who reads the page sees that something is there, not what it is, and can ask to read it ([ADR-0161](../adr/0161-access-requests-are-answered-from-the-task-list.md)); the request becomes a task for the page's owner ([ADR-0162](../adr/0162-an-access-request-becomes-a-task-for-the-pages-owner.md)).

## What you see when you may not read it

- **Protected words** show as a dark bar inside the sentence: `▒▒▒▒▒▒▒▒`. The bar is always the same length, whatever it hides.
- **Protected blocks** show as one dark card, *Protected content.*, in place of all the blocks of the protection. It does not say whether a picture, a table or a whole section is behind it. Pictures and files of protected blocks are not shown anywhere else either: not in the album, the starmap, exports or copies.
- The same holds for a page embedded in another one: its protected parts are dark there too.
- Below the text, the card *Protected passages* lists each protection of the page by number ("Passage 1", "Protected blocks 2") and says who decides about it.

While a page holds content you may not read, you cannot change its text; you can still change its title, fields, state, labels and comments.

## How to ask for access

1. Click the dark bar or card — or move to it with the Tab key and press Enter. This works on the page itself and inside embedded pages, with the mouse or on a touch screen.
2. A dialog says what is protected (for example *Protected blocks 1 of "Editortest"*) and who decides: the person who protected it, by name, and the administrators of the room.
3. Choose **Ask for access**. The dialog confirms the request and names the task made for it, and the person it is assigned to.

You need no right to edit the room to ask: reading the page is enough. A request carries no message; if you want to explain why you need it, add a comment to its task, which you can read.

If you have asked already, clicking again says when you asked and for whom the request waits. A request cannot be withdrawn.

## Where requests appear, and who decides

Each request becomes a **task**, in the same room as the page and below it. You do not need the right to create anything in the room for that: the wiki makes the task for you. Its history says it was made by the access request, for you.

- **Who gets it.** The task is assigned to the page's **owner** (its field *owner*), if one is set who can read the page. Otherwise it goes to the person who protected the content.
- **Who decides.** The person who protected the content and the administrators of the room decide, as before; they also change who reads it, take the protection off or erase it. The page's **owner** may grant or decline requests of others too, but not their own, and does not change the readers. While a page holds protected content, its owner is set only by people who read all of it, or by the room's administrators.
- **Who sees it.** Only the person it is assigned to, you, the person who protected the content, the page's owner and the room's administrators. Everyone else in the room does not see that a request was made.
- **What it says.** Its title is *Access request: you for «page»*, in the language its assignee chose. It names the protection by number, as the page's card does, and links the page. It never shows the protected content.
- **Where it is found.** In **Tasks**, under *Assigned to me*, with the label `access-request` to filter by, and on boards. The person it is assigned to and you both follow the task, so you hear of each other's comments; use them to ask back or explain. Those who decide also get a notification.

**Grant** or **Decline** with the buttons on the task. Grant lets the person read the protection from then on; Decline leaves it protected for them. Either way the task gets a comment saying who decided, it is finished by its workflow, and the person who asked is notified. Closing the task without deciding counts as declining. A decision made elsewhere — taking the protection off, adding the person as a reader — finishes the task too.

The page's card *Protected passages* no longer has buttons for such requests: it says who asked and when, and links the task. Requests made before tasks were — or in an installation without a type that can hold a task — are still answered with Grant and Decline in the card.

Agents find the tasks like any task, for example with the MCP tool `query_items` or `find_items` by the label `access-request`; they cannot grant or decline.

## What the one who asked sees afterwards

- While the request waits, the dark bar says *Asked; waiting for an answer*, and the page's card names your task and to whom it is assigned.
- When it is answered, they get a notification: "lets you read a protected passage of …" or "declined your request for a protected passage of …".
- After a grant the content appears in place of the bar the next time the page is shown. After a decline the bar stays and they may ask again; that makes a new task.
