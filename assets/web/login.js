const loginForm = document.getElementById("loginForm");
const usernameInput = document.getElementById("username");
const passwordInput = document.getElementById("password");
const errorMessage = document.getElementById("errorMessage");

loginForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  errorMessage.style.display = "none";

  try {
    const response = await fetch("/api/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        username: usernameInput.value,
        password: passwordInput.value,
      }),
    });

    if (response.ok) {
      window.location.href = "/index.html";
      return;
    }

    errorMessage.style.display = "block";
  } catch {
    errorMessage.textContent = "An error occurred. Please try again.";
    errorMessage.style.display = "block";
  }
});
